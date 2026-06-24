package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"laz/internal/server/config"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestOpenStoreRequiresSQLiteAuthData(t *testing.T) {
	_, err := openStore(config.Config{Storage: "sqlite", DataPath: t.TempDir() + "/state.db"})
	if err == nil || !strings.Contains(err.Error(), "LAZ_AUTH_DATA is required") {
		t.Fatalf("expected missing auth data error, got %v", err)
	}
}

func TestAgentGRPCTLSConfigAcceptsActiveInstallationLeaf(t *testing.T) {
	serverCert, serverKey := testCertificate(t, "server", false)
	clientCert, clientKey := testCertificate(t, "installation", true)
	serverCertFile := writePEMFile(t, "server.crt", serverCert)
	serverKeyFile := writePEMFile(t, "server.key", serverKey)
	clientTLSCert, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(clientTLSCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	node, instance, installation, err := st.CreateNodeWithInstanceAndAgentInstallation(
		model.Node{ID: "node-1", Name: "node", Type: model.NodeTypeNativeHy2},
		model.NodeInstance{ID: "instance-1", Host: "127.0.0.1"},
		model.AgentInstallation{ID: "installation-1", AgentType: "hysteria2", CertificateFingerprintSHA256: fmt.Sprintf("%x", sha256.Sum256(leaf.Raw)), CertificateSerial: leaf.SerialNumber.String(), CertificateNotBefore: leaf.NotBefore, CertificateNotAfter: leaf.NotAfter},
	)
	if err != nil || node.ID == "" || instance.ID == "" || installation.ID == "" {
		t.Fatalf("create installation: node=%+v instance=%+v installation=%+v err=%v", node, instance, installation, err)
	}
	tlsConfig, err := agentGRPCTLSConfig(config.Config{AgentGRPCCertFile: serverCertFile, AgentGRPCKeyFile: serverKeyFile}, st)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			err = conn.(*tls.Conn).Handshake()
			_ = conn.Close()
		}
		accepted <- err
	}()
	clientConfig := &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{clientTLSCert}}
	conn, err := tls.Dial("tcp", listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("installation leaf handshake failed: %v", err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func testCertificate(t *testing.T, commonName string, client bool) ([]byte, []byte) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: commonName}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames = []string{"localhost"}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
}

func writePEMFile(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenStoreRequiresPostgresAuthDatabaseURL(t *testing.T) {
	_, err := openStore(config.Config{Storage: "postgres", DatabaseURL: "postgres://example"})
	if err == nil || !strings.Contains(err.Error(), "LAZ_AUTH_DATABASE_URL is required") {
		t.Fatalf("expected missing auth database URL error, got %v", err)
	}
}
