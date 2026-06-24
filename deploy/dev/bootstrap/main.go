// lab-init only prepares disposable local development data, never production nodes.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
	"laz/internal/protocol/hy2config"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
	"laz/internal/server/storage"
)

func main() {
	if err := run("/lab"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root string) error {
	ready := filepath.Join(root, "server", ".ready")
	if marker, err := os.ReadFile(ready); err == nil {
		if string(marker) != "initialized-awg31\n" {
			return fmt.Errorf("existing lab uses the old Docker AWG runtime; preserve it with a different compose project name or explicitly reset disposable volumes with compose down -v")
		}
		fmt.Println("Development data already initialized; preserving it.")
		return nil
	}
	for _, node := range []string{"server", "hysteria", "amnezia"} {
		if err := os.MkdirAll(filepath.Join(root, node), 0750); err != nil {
			return err
		}
	}
	database := filepath.Join(root, "server", "laz.db")
	if _, err := os.Stat(database); err == nil {
		return fmt.Errorf("partial lab initialization detected; use compose down -v to reset this disposable lab")
	}
	db, err := store.OpenSQLiteWithAuth(database, filepath.Join(root, "server", "auth.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	secret, err := randomSecret()
	if err != nil {
		return err
	}
	st, err := store.WrapSecrets(db, secret)
	if err != nil {
		return err
	}
	write := func(node, name string, raw []byte) error {
		return os.WriteFile(filepath.Join(root, node, name), raw, 0640)
	}
	serverCert, serverKey, _, err := certificate("server", true)
	if err != nil {
		return err
	}
	if err = write("server", "server.crt", serverCert); err != nil {
		return err
	}
	if err = write("server", "server.key", serverKey); err != nil {
		return err
	}
	if err = write("server", "runtime.env", []byte("LAZ_SECRET_KEY='"+secret+"'\n")); err != nil {
		return err
	}
	statsSecret, err := randomSecret()
	if err != nil {
		return err
	}
	for _, name := range []string{"hysteria", "amnezia"} {
		typ, agentType := model.NodeTypeNativeHy2, "hysteria2"
		if name == "amnezia" {
			typ = model.NodeTypeNativeAmneziaWG
			agentType = "amneziawg"
		}
		installID := store.NewID("ain")
		cert, key, leaf, err := certificate(installID, false)
		if err != nil {
			return err
		}
		fingerprint := sha256.Sum256(leaf.Raw)
		node, _, installation, err := st.CreateNodeWithInstanceAndAgentInstallation(
			model.Node{Name: "dev-" + name, Type: typ, Region: "LOCAL", Status: model.StatusActive},
			model.NodeInstance{Host: name, SSHPort: 22, SSHUser: "root", Status: model.StatusActive},
			model.AgentInstallation{ID: installID, AgentType: agentType, Status: model.StatusActive, CertificateFingerprintSHA256: hex.EncodeToString(fingerprint[:]), CertificateSerial: leaf.SerialNumber.String(), CertificateNotBefore: leaf.NotBefore, CertificateNotAfter: leaf.NotAfter})
		if err != nil {
			return err
		}
		config := map[string]any{
			"node_id": node.ID, "installation_id": installation.ID, "agent_type": agentType,
			"server_url": "http://server:8088", "agent_grpc_target": "server:9443",
			"state_path": "/var/lib/lazgate-agent/state.db", "transport_path": "/var/lib/lazgate-agent/transport.db",
			"mtls": map[string]string{"ca_file": "/lab/server.crt", "cert_file": "/lab/node.crt", "key_file": "/lab/node.key"},
			"sync": map[string]int{"telemetry_collect_interval_seconds": 30, "auth_sync_interval_seconds": 30, "heartbeat_interval_seconds": 30},
		}
		if name == "hysteria" {
			config["hysteria2"] = map[string]string{"auth_listen": "127.0.0.1:28262", "stats_url": "http://127.0.0.1:25413", "stats_secret": statsSecret, "service_name": "hysteria-server", "config_path": "/etc/hysteria/config.yaml"}
			settings := hy2config.Defaults()
			settings.ProtectInternal = false
			rawSettings, _ := json.Marshal(settings)
			if _, err = st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "localhost", EndpointPort: port("LAZ_DEV_HY2_PORT", 18443), SettingsJSON: string(rawSettings)}); err != nil {
				return err
			}
			tlsCert, tlsKey, _, err := certificate("localhost", true)
			if err != nil {
				return err
			}
			if err = write(name, "tls.crt", tlsCert); err != nil {
				return err
			}
			if err = write(name, "tls.key", tlsKey); err != nil {
				return err
			}
			runtime := map[string]any{"listen": ":443", "tls": map[string]string{"cert": "/lab/tls.crt", "key": "/lab/tls.key"}, "auth": map[string]any{"type": "http", "http": map[string]any{"url": "http://127.0.0.1:28262/auth"}}, "trafficStats": map[string]string{"listen": "127.0.0.1:25413", "secret": statsSecret}}
			raw, err := yaml.Marshal(runtime)
			if err != nil {
				return err
			}
			if err = write(name, "hysteria.yaml", raw); err != nil {
				return err
			}
		} else {
			config["amneziawg"] = map[string]any{"interface_name": "awg0", "service_name": "lab-awg", "config_path": "/var/lib/lazgate-agent/awg/awg0.conf", "awg_binary_path": "/usr/local/bin/awg", "use_sudo": false}
			private, public, err := nativeawg.GenerateKeyPair()
			if err != nil {
				return err
			}
			headerKey, _, err := nativeawg.GenerateKeyPair()
			if err != nil {
				return err
			}
			_, err = st.CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig{NodeID: node.ID, EndpointHost: "localhost", EndpointPort: port("LAZ_DEV_AWG_PORT", 18520), ListenPort: 51820, ServerPrivateKey: private, ServerPublicKey: public, HeaderProtectionKey: headerKey, ServerIPv4: "10.8.1.1", ClientIPv4CIDR: "10.8.1.0/24", FirstClientIPv4: "10.8.1.2", LastClientIPv4: "10.8.1.254", DNS: "1.1.1.1", MTU: 1376, PersistentKeepalive: 25, ClientRouteAllowedIPs: "0.0.0.0/0", JC: 4, JMin: 40, JMax: 70, S1: 32, S2: 48, S3: 16, S4: 16, H1: "1001", H2: "1002", H3: "1003", H4: "1004"})
			if err != nil {
				return err
			}
			conf := fmt.Sprintf("[Interface]\nPrivateKey = %s\nHeaderProtectionKey = %s\nListenPort = 51820\nJc = 4\nJmin = 40\nJmax = 70\nS1 = 32\nS2 = 48\nS3 = 16\nS4 = 16\nH1 = 1001\nH2 = 1002\nH3 = 1003\nH4 = 1004\n", private, headerKey)
			if err = write(name, "awg.conf", []byte(conf)); err != nil {
				return err
			}
		}
		raw, err := yaml.Marshal(config)
		if err != nil {
			return err
		}
		for filename, data := range map[string][]byte{"agent.yaml": raw, "server.crt": serverCert, "node.crt": cert, "node.key": key} {
			if err = write(name, filename, data); err != nil {
				return err
			}
		}
		password, err := randomSecret()
		if err != nil {
			return err
		}
		if err = write(name, "root-password", []byte(password)); err != nil {
			return err
		}
	}
	if err = db.Close(); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(path, 10001, 10001)
		}); err != nil {
			return err
		}
	}
	if err = os.WriteFile(ready, []byte("initialized-awg31\n"), 0640); err != nil {
		return err
	}
	fmt.Println("Lab initialized. Admin bootstrap invite will appear in server logs.")
	return nil
}
func randomSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b[:]), nil
}
func port(name string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 && v <= 65535 {
		return v
	}
	return fallback
}
func certificate(name string, server bool) ([]byte, []byte, *x509.Certificate, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature}
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	if server {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames = []string{name, "localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, nil, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), leaf, nil
}
