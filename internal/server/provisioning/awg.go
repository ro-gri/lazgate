package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"

	"laz/internal/server/amneziasettings"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
	storeutil "laz/internal/server/storage"
)

const AWGInstallerVersion = "v6.1.1"
const awgAdmin = "lazgate-awg-admin"
const awgRoot = "/var/lib/lazgate-provision"

var AWGSteps = []string{"awg.install.prepare", "awg.install.launch", "awg.install.poll", "awg.install.handoff", "awg.install.register", "awg.install.agent", "awg.install.verify", "awg.install.wait_agent", "awg.install.done"}

type AWGRequest struct {
	Settings            *amneziasettings.Input `json:"settings,omitempty"`
	AllowPackageUpgrade bool                   `json:"allow_package_upgrade"`
	SSHHost             string                 `json:"ssh_host"`
	SSHPort             int                    `json:"ssh_port"`
	BootstrapUser       string                 `json:"bootstrap_user"`
	BootstrapPassword   string                 `json:"bootstrap_password"`
	NodeName            string                 `json:"node_name"`
	Region              string                 `json:"region"`
	EndpointHost        string                 `json:"endpoint_host"`
	ListenPort          int                    `json:"listen_port"`
	Subnet              string                 `json:"subnet"`
}

// AWGInstallInput deliberately cannot hold the initial SSH credentials.
type AWGInstallInput struct {
	Settings         *amneziasettings.Input `json:"settings,omitempty"`
	SSHHost          string                 `json:"ssh_host"`
	SSHPort          int                    `json:"ssh_port"`
	NodeName         string                 `json:"node_name"`
	Region           string                 `json:"region"`
	EndpointHost     string                 `json:"endpoint_host"`
	ListenPort       int                    `json:"listen_port"`
	Subnet           string                 `json:"subnet"`
	InterfaceName    string                 `json:"interface_name"`
	InstallerVersion string                 `json:"installer_version"`
}

// AWGState is carried only in the encrypted persistent outbox, never in API responses.
type AWGState struct {
	EventPrincipal string                    `json:"event_principal"`
	Input          AWGInstallInput           `json:"input"`
	Identity       InstallState              `json:"identity"`
	Config         model.AmneziaWGNodeConfig `json:"config"`
	ServerURL      string                    `json:"server_url"`
	AgentTarget    string                    `json:"agent_target"`
	DownloadBase   string                    `json:"download_base"`
	BootID         string                    `json:"boot_id"`
	Runs           int                       `json:"runs"`
	Deadline       time.Time                 `json:"deadline"`
}

type AWGStepResult struct {
	State AWGState
	Next  string
	Wait  time.Duration
	Done  bool
}

type awgStore interface {
	GetNode(string) (model.Node, error)
	CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error)
	GetAmneziaWGNodeConfig(string) (model.AmneziaWGNodeConfig, error)
}

func (i *Installer) BootstrapAWG(ctx context.Context, in AWGRequest, serverURL, target, downloads string) (AWGState, error) {
	if err := i.validateAgentServerTrustMaterial(); err != nil {
		return AWGState{}, err
	}
	if _, ok := i.store.(awgStore); !ok {
		return AWGState{}, errors.New("AWG storage is unavailable")
	}
	var metadataErr error
	in.SSHHost, metadataErr = normalizeServerIP(in.SSHHost)
	if metadataErr != nil {
		return AWGState{}, metadataErr
	}
	in.Region, metadataErr = normalizeServerRegion(in.Region)
	if metadataErr != nil {
		return AWGState{}, metadataErr
	}
	if !in.AllowPackageUpgrade {
		return AWGState{}, errors.New("v6.1.1 upgrades OS packages and reboots; allow_package_upgrade must explicitly consent")
	}
	if in.SSHPort == 0 {
		in.SSHPort = 22
	}
	if in.BootstrapUser == "" {
		in.BootstrapUser = "root"
	}
	if in.ListenPort == 0 {
		in.ListenPort = 51820
	}
	if in.Subnet == "" {
		in.Subnet = "10.8.1.0/24"
	}
	if in.EndpointHost == "" {
		in.EndpointHost = in.SSHHost
	}
	if in.Settings != nil {
		settings := *in.Settings
		settings.Version = 0
		if settings.EndpointHost == "" {
			settings.EndpointHost = in.SSHHost
		}
		key, err := nativeawg.GeneratePresharedKey()
		if err != nil {
			return AWGState{}, err
		}
		preview, err := settings.Apply(model.AmneziaWGNodeConfig{ServerPrivateKey: "installer-generated", ServerPublicKey: "installer-generated", HeaderProtectionKey: key})
		if err != nil {
			return AWGState{}, err
		}
		in.EndpointHost, in.ListenPort, in.Subnet = preview.EndpointHost, preview.ListenPort, preview.ClientIPv4CIDR
		in.Settings = &settings
	}
	if !validAWGHost(in.EndpointHost) || !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(in.BootstrapUser) {
		return AWGState{}, errors.New("invalid SSH host, endpoint host or bootstrap user")
	}
	if len(in.NodeName) > 128 || strings.ContainsAny(in.NodeName, "\r\n\x00\\\"'") {
		return AWGState{}, errors.New("invalid node name")
	}
	if _, err := parseAWGSubnet(in.Subnet); err != nil {
		return AWGState{}, err
	}
	if in.ListenPort < 1 || in.ListenPort > 65535 || in.SSHPort < 1 || in.SSHPort > 65535 || in.SSHHost == "" || in.NodeName == "" || in.BootstrapPassword == "" {
		return AWGState{}, errors.New("valid SSH host, ports, password and node name are required")
	}
	if strings.ContainsAny(in.EndpointHost, "\r\n\x00 /:'\"") {
		return AWGState{}, errors.New("invalid endpoint host")
	}
	if !strings.HasPrefix(downloads, "https://") {
		return AWGState{}, errors.New("agent download base must use HTTPS")
	}
	private, public, err := generateSSHKey()
	if err != nil {
		return AWGState{}, err
	}
	id := storeutil.NewID("ain")
	certs, err := generateMTLSFiles(id)
	if err != nil {
		return AWGState{}, err
	}
	input := AWGInstallInput{SSHHost: in.SSHHost, SSHPort: in.SSHPort, NodeName: in.NodeName, Region: in.Region, EndpointHost: in.EndpointHost, ListenPort: in.ListenPort, Subnet: in.Subnet, InterfaceName: "awg0", InstallerVersion: AWGInstallerVersion}
	input.Settings = in.Settings
	state := AWGState{Input: input, ServerURL: serverURL, AgentTarget: target, DownloadBase: downloads, Deadline: time.Now().UTC().Add(90 * time.Minute), Identity: InstallState{NodeID: NewNodeID(), NodeInstanceID: storeutil.NewID("nins"), InstallationID: id, AgentType: "amneziawg", PrivateKey: private, PublicKey: public, CAPEM: certs.CAPEM, NodeCertPEM: certs.NodeCertPEM, NodeKeyPEM: certs.NodeKeyPEM}}
	return i.bootstrapAWGHost(ctx, state, sshConfig{Host: in.SSHHost, Port: in.SSHPort, User: in.BootstrapUser, Password: in.BootstrapPassword})
}

// Like Hysteria bootstrap, credentials live only in the initiating request.
func (i *Installer) bootstrapAWGHost(ctx context.Context, state AWGState, bootstrap sshConfig) (AWGState, error) {
	in := state.Input
	conn, err := dialSSH(ctx, bootstrap)
	if err != nil {
		return state, &awgConnectionError{err}
	}
	defer conn.Close()
	routes, err := runSSH(ctx, conn, "ip -j -4 route show")
	if err != nil {
		return state, err
	}
	network, err := netip.ParsePrefix(in.Subnet)
	if err != nil {
		return state, err
	}
	if err = checkAWGRoutes(routes, network); err != nil {
		return state, err
	}
	// The root-only ownership marker permits replay after a partial bootstrap,
	// but never permits taking over another installation.
	script := fmt.Sprintf(`set -eu
owner=%s
if sudo -n test -f /var/lib/lazgate-provision/owner; then
 test "$(sudo -n cat /var/lib/lazgate-provision/owner)" = "$owner"
else
%s
 sudo -n install -d -m 700 /var/lib/lazgate-provision
 printf '%%s\n' "$owner" | sudo -n tee /var/lib/lazgate-provision/owner >/dev/null
 sudo -n chmod 600 /var/lib/lazgate-provision/owner
fi
sudo -n useradd -m -s /bin/bash %s 2>/dev/null || sudo -n id %s >/dev/null
sudo -n install -d -m 700 -o %s -g %s /home/%s/.ssh
printf '%%s\n' %s | sudo -n tee /home/%s/.ssh/authorized_keys >/dev/null
sudo -n chown %s:%s /home/%s/.ssh/authorized_keys
sudo -n chmod 600 /home/%s/.ssh/authorized_keys
printf '%%s ALL=(root) NOPASSWD: ALL\n' %s | sudo -n tee /etc/sudoers.d/%s >/dev/null
sudo -n chmod 440 /etc/sudoers.d/%s
`, shellQuote(state.Identity.InstallationID), awgPreflight(in), awgAdmin, awgAdmin, awgAdmin, awgAdmin, awgAdmin, shellQuote(state.Identity.PublicKey), awgAdmin, awgAdmin, awgAdmin, awgAdmin, awgAdmin, awgAdmin, awgAdmin, awgAdmin)
	if _, err = runSSH(ctx, conn, script); err != nil {
		return state, err
	}
	if _, err = i.awgRun(ctx, state, "true"); err != nil {
		return state, err
	}
	return state, nil
}

func validAWGHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Is4()
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`).MatchString(label) {
			return false
		}
	}
	return true
}

func awgPreflight(in AWGInstallInput) string {
	return fmt.Sprintf(`set -eu
sudo -n true
test -d /run/systemd/system || { echo UNSUPPORTED_OS >&2; exit 1; }
`+awgSelectPlatformScript()+`
test "$(uname -m)" = x86_64 || { echo UNSUPPORTED_OS >&2; exit 1; }
dpkg --compare-versions "$(uname -r | cut -d- -f1)" ge 6.7 || { echo UNSUPPORTED_KERNEL >&2; exit 1; }
test -d /lib/modules/"$(uname -r)"/build || apt-cache show "linux-headers-$(uname -r)" >/dev/null || { echo MISSING_KERNEL_HEADERS >&2; exit 1; }
apt-cache show dkms >/dev/null
command -v curl >/dev/null
command -v sha256sum >/dev/null
command -v systemd-run >/dev/null
if command -v ufw >/dev/null && sudo -n ufw status | grep -q '^Status: active'; then echo FIREWALL_CONFIGURATION_FAILED >&2; exit 1; fi
if sudo -n systemctl is-active --quiet firewalld; then echo FIREWALL_CONFIGURATION_FAILED >&2; exit 1; fi
if command -v nft >/dev/null && sudo -n nft list tables | grep -Ev '^table (ip|ip6) (filter|nat|mangle|raw|security)$' | grep -q .; then echo FIREWALL_CONFIGURATION_FAILED >&2; exit 1; fi
if sudo -n test -e /root/awg || sudo -n test -e /etc/amnezia/amneziawg/awg0.conf || ip link show awg0 >/dev/null 2>&1; then echo 'AWG_INTERFACE_FAILED: existing installation requires manual attach; refusing overwrite' >&2; exit 1; fi
test ! -e /etc/lazgate-agent
! id %s >/dev/null 2>&1
! ss -H -lun | awk '{print $5}' | grep -Eq ':%d$'
! ip -4 route show | grep -F %s
`, awgAdmin, in.ListenPort, shellQuote(in.Subnet))
}

func checkAWGRoutes(raw string, subnet netip.Prefix) error {
	var routes []struct {
		Dst string `json:"dst"`
	}
	if err := json.Unmarshal([]byte(raw), &routes); err != nil {
		return errors.New("invalid host route response")
	}
	for _, route := range routes {
		if route.Dst == "" || route.Dst == "default" {
			continue
		}
		prefix, err := netip.ParsePrefix(route.Dst)
		if err != nil {
			continue
		}
		if prefix.Overlaps(subnet) {
			return errors.New("AWG_INTERFACE_FAILED: VPN subnet overlaps an existing route")
		}
	}
	return nil
}

func (i *Installer) awgRun(ctx context.Context, s AWGState, script string) (string, error) {
	if i.awgExecute != nil {
		return i.awgExecute(ctx, s, script)
	}
	conn, err := dialSSH(ctx, sshConfig{Host: s.Input.SSHHost, Port: s.Input.SSHPort, User: awgAdmin, PrivateKey: s.Identity.PrivateKey})
	if err != nil {
		return "", &awgConnectionError{err}
	}
	defer conn.Close()
	return runSSH(ctx, conn, script)
}

type awgConnectionError struct{ error }

func retryableAWGSSH(err error) bool {
	var connection *awgConnectionError
	var missing *ssh.ExitMissingError
	var network net.Error
	return errors.As(err, &connection) || errors.As(err, &missing) || errors.As(err, &network)
}

func (i *Installer) ExecuteAWGStep(ctx context.Context, step string, s AWGState) (AWGStepResult, error) {
	result := AWGStepResult{State: s}
	if time.Now().After(s.Deadline) {
		return result, errors.New("REBOOT_TIMEOUT: provisioning deadline exceeded")
	}
	var err error
	switch step {
	case "awg.install.prepare":
		var out string
		out, err = i.awgRun(ctx, s, awgPrepareScript(s))
		s.BootID = strings.TrimSpace(out)
		result.Next = "awg.install.launch"
	case "awg.install.launch":
		if s.Runs >= 4 {
			return result, errors.New("INSTALLER_FAILED: installer resume limit exceeded")
		}
		_, err = i.awgRun(ctx, s, awgLaunchScript(s))
		if err == nil {
			s.Runs++
		}
		result.Next = "awg.install.poll"
		result.Wait = 10 * time.Second
	case "awg.install.poll":
		var out string
		out, err = i.awgRun(ctx, s, `sudo -n sh -c 'cat /proc/sys/kernel/random/boot_id; if test -f /root/awg/setup_state; then cat /root/awg/setup_state; else echo absent; fi; cat /var/lib/lazgate-provision/exit-code 2>/dev/null || echo running'`)
		if err != nil {
			if !retryableAWGSSH(err) {
				break
			}
			result.Next = step
			result.Wait = 10 * time.Second
			err = nil
			break
		}
		fields := strings.Fields(out)
		if len(fields) != 3 {
			err = errors.New("INSTALLER_FAILED: malformed installer status")
			break
		}
		switch {
		// The upstream finish step removes setup_state before exiting successfully.
		case (fields[1] == "absent" || fields[1] == "99") && fields[2] == "0":
			result.Next = "awg.install.handoff"
		case fields[0] != s.BootID && (fields[1] == "2" || fields[1] == "3"):
			s.BootID = fields[0]
			result.Next = "awg.install.launch"
		case fields[2] == "running" || ((fields[1] == "2" || fields[1] == "3") && fields[2] == "1"):
			result.Next = step
			result.Wait = 10 * time.Second
		default:
			err = fmt.Errorf("INSTALLER_FAILED: step %s exited %s; inspect root-only installer journal", fields[1], fields[2])
		}
	case "awg.install.handoff":
		var raw string
		raw, err = i.awgRun(ctx, s, awgHandoffScript(s))
		if err == nil {
			s.Config, err = importAWGConfig(raw, s.Input)
			if err == nil && s.Input.Settings != nil {
				s.Config, err = s.Input.Settings.Apply(s.Config)
			}
			s.Config.NodeID = s.Identity.NodeID
		}
		result.Next = "awg.install.register"
	case "awg.install.register":
		s, err = i.registerAWG(s)
		result.Next = "awg.install.agent"
	case "awg.install.agent":
		var script string
		script, err = i.awgAgentScript(s)
		if err == nil {
			_, err = i.awgRun(ctx, s, script)
		}
		result.Next = "awg.install.verify"
	case "awg.install.verify":
		_, err = i.awgRun(ctx, s, awgVerifyScript(s))
		result.Next = "awg.install.wait_agent"
	case "awg.install.wait_agent":
		runtime, e := i.store.GetNodeRuntime(s.Identity.InstallationID)
		if e != nil || runtime.AgentStatus != "online" || runtime.ServiceStatus != "active" || runtime.LastHeartbeatAt.IsZero() || time.Since(runtime.LastHeartbeatAt) > 90*time.Second {
			result.Next = step
			result.Wait = 5 * time.Second
		} else {
			if s.Input.Settings != nil {
				config, configErr := i.store.(awgStore).GetAmneziaWGNodeConfig(s.Identity.NodeID)
				if configErr != nil || config.AppliedConfigVersionMS < config.ConfigVersionMS {
					if configErr == nil && config.ConfigErrorID != "" {
						err = errors.New("AWG_INTERFACE_FAILED: settings were not applied")
						break
					}
					result.Next = step
					result.Wait = 5 * time.Second
					break
				}
			}
			result.Next = "awg.install.done"
		}
	case "awg.install.done":
		result.Done = true
	default:
		err = errors.New("unknown AWG provisioning step")
	}
	result.State = s
	if retryableAWGSSH(err) {
		result.Next = step
		result.Wait = 10 * time.Second
		return result, nil
	}
	// Remote diagnostics may contain generated key material. Never persist raw output.
	if err != nil {
		return result, fmt.Errorf("%s: check the root-only host journal", awgFailureCode(step, err))
	}
	return result, nil
}

func awgFailureCode(step string, err error) string {
	for _, code := range []string{"UNSUPPORTED_OS", "UNSUPPORTED_KERNEL", "MISSING_KERNEL_HEADERS", "DKMS_BUILD_FAILED", "REBOOT_TIMEOUT", "SSH_CONNECTION_FAILED", "AWG_INTERFACE_FAILED", "FIREWALL_CONFIGURATION_FAILED", "AGENT_INSTALL_FAILED", "AGENT_CONNECTION_FAILED", "INSTALLER_FAILED"} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	switch step {
	case "awg.install.prepare":
		return "INSTALLER_DOWNLOAD_FAILED"
	case "awg.install.agent":
		return "AGENT_INSTALL_FAILED"
	case "awg.install.verify", "awg.install.handoff":
		return "AWG_INTERFACE_FAILED"
	case "awg.install.wait_agent":
		return "AGENT_CONNECTION_FAILED"
	default:
		return "INSTALLER_FAILED"
	}
}

func importAWGConfig(raw string, in AWGInstallInput) (model.AmneziaWGNodeConfig, error) {
	file, err := ini.LoadSources(ini.LoadOptions{AllowNonUniqueSections: true, IgnoreInlineComment: true, IgnoreContinuation: true, KeyValueDelimiters: "="}, []byte(raw))
	if err != nil {
		return model.AmneziaWGNodeConfig{}, errors.New("invalid installer configuration")
	}
	sections, err := file.SectionsByName("Interface")
	if err != nil || len(sections) != 1 {
		return model.AmneziaWGNodeConfig{}, errors.New("installer must define one Interface section")
	}
	values := sections[0].KeysHash()
	pool, err := parseAWGSubnet(in.Subnet)
	if err != nil {
		return model.AmneziaWGNodeConfig{}, err
	}
	c := model.AmneziaWGNodeConfig{EndpointHost: in.EndpointHost, EndpointPort: in.ListenPort, ListenPort: in.ListenPort, ServerIPv4: pool.Server.String(), ClientIPv4CIDR: in.Subnet, FirstClientIPv4: pool.First.String(), LastClientIPv4: pool.Last.String(), ServerPrivateKey: values["PrivateKey"], HeaderProtectionKey: values["HeaderProtectionKey"], DNS: nativeawg.DefaultDNS, MTU: nativeawg.DefaultMTU, ConfigVersionMS: time.Now().UnixMilli()}
	c.ContentPaddingAddition = values["ContentPaddingAddition"]
	if values["ListenPort"] != strconv.Itoa(in.ListenPort) || values["Address"] != netip.PrefixFrom(pool.Server, pool.Network.Bits()).String() {
		return c, errors.New("installer interface address/port mismatch")
	}
	if values["MTU"] != "" {
		c.MTU, _ = strconv.Atoi(values["MTU"])
	}
	for _, key := range []string{"I1", "I2", "I3", "I4", "I5"} {
		if values[key] != "" {
			return c, errors.New("CPS must be disabled")
		}
	}
	c.ServerPublicKey, err = nativeawg.PublicKeyFromPrivate(c.ServerPrivateKey)
	if err != nil {
		return c, err
	}
	c.H1, c.H2, c.H3, c.H4 = values["H1"], values["H2"], values["H3"], values["H4"]
	for name, p := range map[string]*int{"Jc": &c.JC, "Jmin": &c.JMin, "Jmax": &c.JMax, "S1": &c.S1, "S2": &c.S2, "S3": &c.S3, "S4": &c.S4} {
		*p, err = strconv.Atoi(values[name])
		if err != nil {
			return c, fmt.Errorf("invalid %s", name)
		}
	}
	if c.HeaderProtectionKey == "" {
		return c, errors.New("AWG 3.1 key missing")
	}
	return c, nativeawg.ValidateNodeConfig(c)
}

func (i *Installer) registerAWG(s AWGState) (AWGState, error) {
	st := i.store.(awgStore)
	cert, err := parseMTLSCertificate(s.Identity.NodeCertPEM)
	if err != nil {
		return s, err
	}
	node, err := st.GetNode(s.Identity.NodeID)
	if err != nil {
		node, _, _, err = i.store.CreateNodeWithInstanceAndAgentInstallation(model.Node{ID: s.Identity.NodeID, Name: s.Input.NodeName, Region: s.Input.Region, Type: model.NodeTypeNativeAmneziaWG}, model.NodeInstance{ID: s.Identity.NodeInstanceID, NodeID: s.Identity.NodeID, Host: s.Input.SSHHost, SSHPort: s.Input.SSHPort, SSHUser: awgAdmin, SSHKey: s.Identity.PrivateKey}, model.AgentInstallation{ID: s.Identity.InstallationID, NodeInstanceID: s.Identity.NodeInstanceID, AgentType: "amneziawg", CertificateFingerprintSHA256: certificateFingerprint(cert), CertificateSerial: cert.SerialNumber.String(), CertificateNotBefore: cert.NotBefore, CertificateNotAfter: cert.NotAfter})
	}
	if err != nil {
		return s, err
	}
	s.Identity.Node = node
	if _, err = st.GetAmneziaWGNodeConfig(node.ID); err != nil {
		s.Config, err = st.CreateAmneziaWGNodeConfig(s.Config)
	}
	return s, err
}
