package provisioning

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"laz/internal/server/integrations/nativehy2"
	"laz/internal/server/model"
	storeutil "laz/internal/server/storage"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Store interface {
	CreateNode(model.Node) (model.Node, error)
	GetNodeRuntime(nodeID string) (model.NodeRuntime, error)
}

type Installer struct {
	store              Store
	agentServerCertPEM string
}

func New(st Store) *Installer {
	return &Installer{store: st}
}

func (i *Installer) SetAgentServerCertPEM(pem string) {
	i.agentServerCertPEM = strings.TrimSpace(pem)
}

type Hysteria2Input struct {
	SSHHost             string `json:"ssh_host"`
	SSHPort             int    `json:"ssh_port"`
	BootstrapUser       string `json:"bootstrap_user"`
	BootstrapPassword   string `json:"bootstrap_password"`
	NodeName            string `json:"node_name"`
	PublicDomain        string `json:"public_domain"`
	HysteriaPort        int    `json:"hysteria_port"`
	MasqueradeURL       string `json:"masquerade_url"`
	InstallVersion      string `json:"install_version"`
	ACMEEmail           string `json:"acme_email"`
	ObfsEnabled         bool   `json:"obfs_enabled"`
	ObfsType            string `json:"obfs_type"`
	TrafficStatsEnabled bool   `json:"traffic_stats_enabled"`
	TrafficStatsListen  string `json:"traffic_stats_listen"`
	ServerURL           string `json:"server_url"`
	AgentGRPCTarget     string `json:"agent_grpc_target"`
	AgentDownloadBase   string `json:"agent_download_base"`
	Progress            func(Step)
}

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepOK      StepStatus = "ok"
	StepFailed  StepStatus = "failed"
)

type Step struct {
	Name      string     `json:"name"`
	Status    StepStatus `json:"status"`
	Message   string     `json:"message,omitempty"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	EndedAt   time.Time  `json:"ended_at,omitempty"`
}

type Result struct {
	Node            model.Node `json:"node"`
	PublicDomain    string     `json:"public_domain"`
	HysteriaPort    int        `json:"hysteria_port"`
	ServiceName     string     `json:"service_name"`
	Steps           []Step     `json:"steps"`
	Logs            []string   `json:"logs"`
	RetrySafe       bool       `json:"retry_safe"`
	GeneratedDomain bool       `json:"generated_domain"`
}

type runner struct {
	installer *Installer
	input     Hysteria2Input
	attach    bool
	steps     []Step
	logs      []string
	secrets   []string
	progress  func(Step)
}

func (i *Installer) InstallHysteria2(ctx context.Context, input Hysteria2Input) (Result, error) {
	input = normalize(input)
	r := &runner{installer: i, input: input, progress: input.Progress}
	r.initSteps()
	result, err := r.run(ctx)
	if err != nil {
		result.Steps = r.steps
		result.Logs = r.logs
		result.RetrySafe = !r.stepSucceeded("Writing Hysteria2 config")
	}
	return result, err
}

func (i *Installer) AttachHysteria2(ctx context.Context, input Hysteria2Input) (Result, error) {
	input = normalize(input)
	r := &runner{installer: i, input: input, attach: true, progress: input.Progress}
	r.initSteps()
	result, err := r.run(ctx)
	if err != nil {
		result.Steps = r.steps
		result.Logs = r.logs
		result.RetrySafe = !r.stepSucceeded("Writing Hysteria2 config")
	}
	return result, err
}

func normalize(input Hysteria2Input) Hysteria2Input {
	input.SSHHost = strings.TrimSpace(input.SSHHost)
	input.BootstrapUser = strings.TrimSpace(input.BootstrapUser)
	input.NodeName = strings.TrimSpace(input.NodeName)
	input.PublicDomain = strings.TrimSpace(input.PublicDomain)
	input.MasqueradeURL = strings.TrimSpace(input.MasqueradeURL)
	input.InstallVersion = strings.TrimSpace(input.InstallVersion)
	input.ACMEEmail = strings.TrimSpace(input.ACMEEmail)
	input.ObfsType = strings.TrimSpace(input.ObfsType)
	input.TrafficStatsListen = strings.TrimSpace(input.TrafficStatsListen)
	input.ServerURL = strings.TrimRight(strings.TrimSpace(input.ServerURL), "/")
	input.AgentGRPCTarget = strings.TrimSpace(input.AgentGRPCTarget)
	input.AgentDownloadBase = strings.TrimRight(strings.TrimSpace(input.AgentDownloadBase), "/")
	if input.SSHPort == 0 {
		input.SSHPort = 22
	}
	if input.BootstrapUser == "" {
		input.BootstrapUser = "root"
	}
	if input.HysteriaPort == 0 {
		input.HysteriaPort = 443
	}
	if input.MasqueradeURL == "" {
		input.MasqueradeURL = "https://news.ycombinator.com/"
	}
	if input.InstallVersion == "" {
		input.InstallVersion = "latest"
	}
	if input.ObfsType == "" {
		input.ObfsType = "salamander"
	}
	if input.TrafficStatsListen == "" {
		input.TrafficStatsListen = "127.0.0.1:25413"
	}
	if input.AgentDownloadBase == "" {
		input.AgentDownloadBase = "https://github.com/ro-gri/lazgate/releases/latest/download"
	}
	return input
}

func (r *runner) initSteps() {
	r.steps = InitialSteps(r.attach)
}

func InitialSteps(attach bool) []Step {
	names := []string{
		"Connecting to VPS",
		"Checking system",
		"Creating lazgate SSH user",
		"Installing Hysteria2",
		"Installing LazGate Agent",
		"Writing Hysteria2 config",
		"Starting Hysteria2 service",
		"Verifying service",
		"Registering node",
		"Waiting for LazGate Agent",
		"Done",
	}
	if attach {
		names = []string{
			"Connecting to VPS",
			"Checking system",
			"Creating lazgate SSH user",
			"Detecting existing Hysteria2",
			"Installing LazGate Agent",
			"Writing Hysteria2 config",
			"Starting Hysteria2 service",
			"Verifying service",
			"Registering node",
			"Waiting for LazGate Agent",
			"Done",
		}
	}
	steps := make([]Step, 0, len(names))
	for _, name := range names {
		steps = append(steps, Step{Name: name, Status: StepPending})
	}
	return steps
}

func (r *runner) run(ctx context.Context) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	generatedDomain := false
	if r.input.PublicDomain == "" {
		r.input.PublicDomain = generatedDomainForHost(r.input.SSHHost)
		generatedDomain = true
	}
	obfsPassword, err := randomSecret()
	if err != nil {
		return Result{}, err
	}
	statsSecret, err := randomSecret()
	if err != nil {
		return Result{}, err
	}
	privateKey, publicKey, err := generateSSHKey()
	if err != nil {
		return Result{}, err
	}
	nodeID := storeutil.NewID("nod")
	certs, err := generateMTLSFiles(nodeID)
	if err != nil {
		return Result{}, err
	}
	r.secrets = append(r.secrets, r.input.BootstrapPassword, obfsPassword, statsSecret, privateKey, certs.NodeKeyPEM)

	r.start("Connecting to VPS")
	bootstrap, err := dialSSH(ctx, sshConfig{
		Host:     r.input.SSHHost,
		Port:     r.input.SSHPort,
		User:     r.input.BootstrapUser,
		Password: r.input.BootstrapPassword,
	})
	if err != nil {
		r.fail("Connecting to VPS", err)
		return Result{}, r.safeError(err)
	}
	r.ok("Connecting to VPS", "Connected to target VPS.")
	defer bootstrap.Close()

	if err := r.preflight(ctx, bootstrap); err != nil {
		return Result{}, err
	}
	if err := r.createUser(ctx, bootstrap, publicKey); err != nil {
		return Result{}, err
	}

	r.start("Creating lazgate SSH user")
	lazSSH, err := dialSSH(ctx, sshConfig{
		Host:       r.input.SSHHost,
		Port:       r.input.SSHPort,
		User:       "lazgate",
		PrivateKey: privateKey,
	})
	if err != nil {
		r.fail("Creating lazgate SSH user", err)
		return Result{}, r.safeError(err)
	}
	r.ok("Creating lazgate SSH user", "Dedicated lazgate SSH user verified.")
	defer lazSSH.Close()

	if err := r.installOrDetectHysteria(ctx, lazSSH); err != nil {
		return Result{}, err
	}
	serviceName, err := r.writeConfig(ctx, lazSSH, obfsPassword, statsSecret)
	if err != nil {
		return Result{}, err
	}
	if err := r.installAgent(ctx, lazSSH, nodeID, serviceName, statsSecret, certs); err != nil {
		return Result{}, err
	}
	if err := r.startService(ctx, lazSSH, serviceName); err != nil {
		return Result{}, err
	}
	if err := r.verify(ctx, lazSSH, serviceName); err != nil {
		return Result{}, err
	}

	r.start("Registering node")
	meta := nativehy2.Metadata{
		PublicDomain: r.input.PublicDomain,
		ListenPort:   r.input.HysteriaPort,
		ServiceName:  serviceName,
		ObfsEnabled:  r.input.ObfsEnabled,
		ObfsType:     r.input.ObfsType,
		ObfsPassword: obfsPassword,
		AgentEnabled: true,
		StatsURL:     "http://" + r.input.TrafficStatsListen,
		NodeCertPEM:  certs.NodeCertPEM,
	}
	rawMeta, _ := json.Marshal(meta)
	node, err := r.installer.store.CreateNode(model.Node{
		ID:         nodeID,
		Name:       r.input.NodeName,
		Type:       model.NodeTypeNativeHy2,
		BaseURL:    "hy2://" + r.input.PublicDomain + ":" + strconv.Itoa(r.input.HysteriaPort),
		APIKey:     string(rawMeta),
		Region:     "",
		SSHHost:    r.input.SSHHost,
		SSHPort:    r.input.SSHPort,
		SSHUser:    "lazgate",
		SSHKeyPath: privateKey,
		UseIPv6:    false,
	})
	if err != nil {
		r.fail("Registering node", err)
		return Result{}, r.safeError(err)
	}
	r.ok("Registering node", "Node registered in LazGate.")
	if err := r.waitAgent(ctx, node.ID); err != nil {
		return Result{}, err
	}
	r.start("Done")
	r.ok("Done", "Hysteria2 VPS node installed and registered.")
	return Result{
		Node:            node,
		PublicDomain:    r.input.PublicDomain,
		HysteriaPort:    r.input.HysteriaPort,
		ServiceName:     serviceName,
		Steps:           r.steps,
		Logs:            r.logs,
		GeneratedDomain: generatedDomain,
	}, nil
}

func (r *runner) waitAgent(ctx context.Context, nodeID string) error {
	r.start("Waiting for LazGate Agent")
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		runtime, err := r.installer.store.GetNodeRuntime(nodeID)
		if err == nil && runtime.AgentStatus == "online" && !runtime.LastHeartbeatAt.IsZero() {
			r.ok("Waiting for LazGate Agent", "Agent connected and heartbeat was received.")
			return nil
		}
		select {
		case <-ctx.Done():
			r.fail("Waiting for LazGate Agent", ctx.Err())
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	err := errors.New("agent did not connect back to LazGate in time")
	r.fail("Waiting for LazGate Agent", err)
	return r.safeError(err)
}

func (r *runner) validate() error {
	if r.input.SSHHost == "" || r.input.BootstrapPassword == "" || r.input.NodeName == "" {
		return errors.New("ssh host, bootstrap password and node name are required")
	}
	if r.input.ServerURL == "" {
		return errors.New("server_url is required")
	}
	if r.input.AgentGRPCTarget != "" {
		if _, _, err := net.SplitHostPort(r.input.AgentGRPCTarget); err != nil {
			return errors.New("agent_grpc_target must be host:port")
		}
	}
	if r.input.SSHPort < 1 || r.input.SSHPort > 65535 || r.input.HysteriaPort < 1 || r.input.HysteriaPort > 65535 {
		return errors.New("ports must be between 1 and 65535")
	}
	if r.input.ACMEEmail != "" {
		if _, err := mail.ParseAddress(r.input.ACMEEmail); err != nil {
			return errors.New("invalid ACME email")
		}
	}
	if _, err := url.ParseRequestURI(r.input.MasqueradeURL); err != nil {
		return errors.New("invalid masquerade URL")
	}
	if r.input.PublicDomain != "" && !validDomain(r.input.PublicDomain) {
		return errors.New("invalid public domain")
	}
	if !strings.Contains(r.input.TrafficStatsListen, "127.0.0.1:") {
		return errors.New("traffic stats listen address must stay on 127.0.0.1")
	}
	return nil
}

func (r *runner) preflight(ctx context.Context, conn *ssh.Client) error {
	r.start("Checking system")
	installGuards := `! command -v hysteria >/dev/null
test ! -e /etc/hysteria/config.yaml`
	if r.attach {
		installGuards = `if ! command -v hysteria >/dev/null && ! systemctl cat hysteria-server >/dev/null 2>&1 && ! systemctl cat hysteria >/dev/null 2>&1; then
  echo "hysteria is not installed" >&2
  exit 22
fi`
	}
	portGuard := fmt.Sprintf(`ss -lun | awk '{print $5}' | grep -Eq ':(%d)$' && exit 20 || true`, r.input.HysteriaPort)
	if r.attach {
		portGuard = "true"
	}
	script := fmt.Sprintf(`set -eu
id -u
test "$(id -u)" = "0"
test -d /run/systemd/system
command -v curl >/dev/null
uname -m
. /etc/os-release
echo "$ID $VERSION_ID"
%s
%s
ss -ltn | awk '{print $4}' | grep -Eq ':(80)$' && exit 21 || true`, installGuards, portGuard)
	if _, err := runSSH(ctx, conn, script); err != nil {
		if strings.Contains(err.Error(), "Process exited with status 20") {
			err = errors.New("selected Hysteria2 UDP port is already in use")
		} else if strings.Contains(err.Error(), "Process exited with status 21") {
			err = errors.New("TCP port 80 is already in use; ACME HTTP challenge needs it")
		} else if strings.Contains(err.Error(), "Process exited with status 22") {
			err = errors.New("existing Hysteria2 installation was not found")
		}
		r.fail("Checking system", err)
		return r.safeError(err)
	}
	if err := r.verifyDomainResolves(); err != nil {
		r.fail("Checking system", err)
		return r.safeError(err)
	}
	r.ok("Checking system", "Linux/systemd/curl checks passed.")
	return nil
}

func (r *runner) createUser(ctx context.Context, conn *ssh.Client, publicKey string) error {
	r.start("Creating lazgate SSH user")
	script := fmt.Sprintf(`set -eu
id lazgate >/dev/null 2>&1 || useradd --system --create-home --home-dir /home/lazgate --shell /bin/bash lazgate
install -d -m 700 -o lazgate -g lazgate /home/lazgate/.ssh
cat > /home/lazgate/.ssh/authorized_keys <<'EOF_KEY'
%s
EOF_KEY
chown lazgate:lazgate /home/lazgate/.ssh/authorized_keys
chmod 600 /home/lazgate/.ssh/authorized_keys
cat > /etc/sudoers.d/lazgate-hysteria <<'EOF_SUDO'
lazgate ALL=(root) NOPASSWD:ALL
EOF_SUDO
chmod 440 /etc/sudoers.d/lazgate-hysteria`, publicKey)
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail("Creating lazgate SSH user", err)
		return r.safeError(err)
	}
	return nil
}

func (r *runner) installOrDetectHysteria(ctx context.Context, conn *ssh.Client) error {
	if r.attach {
		r.start("Detecting existing Hysteria2")
		if _, err := detectService(ctx, conn); err != nil {
			r.fail("Detecting existing Hysteria2", err)
			return r.safeError(err)
		}
		if _, err := runSSH(ctx, conn, `command -v hysteria >/dev/null 2>&1 || systemctl cat hysteria-server >/dev/null 2>&1 || systemctl cat hysteria >/dev/null 2>&1`); err != nil {
			r.fail("Detecting existing Hysteria2", err)
			return r.safeError(errors.New("existing Hysteria2 installation was not found"))
		}
		r.ok("Detecting existing Hysteria2", "Existing Hysteria2 service detected.")
		return nil
	}
	r.start("Installing Hysteria2")
	if _, err := runSSH(ctx, conn, `sudo bash -lc 'bash <(curl -fsSL https://get.hy2.sh/)'`); err != nil {
		r.fail("Installing Hysteria2", err)
		return r.safeError(err)
	}
	r.ok("Installing Hysteria2", "Official Hysteria2 install script completed.")
	return nil
}

func (r *runner) writeConfig(ctx context.Context, conn *ssh.Client, obfsPassword, statsSecret string) (string, error) {
	r.start("Writing Hysteria2 config")
	serviceName, err := detectService(ctx, conn)
	if err != nil {
		r.fail("Writing Hysteria2 config", err)
		return "", r.safeError(err)
	}
	config := r.hysteriaConfig(obfsPassword, statsSecret)
	if r.attach {
		raw, err := runSSH(ctx, conn, "sudo cat /etc/hysteria/config.yaml")
		if err != nil {
			r.fail("Writing Hysteria2 config", err)
			return "", r.safeError(err)
		}
		patched, detectedPort, hasStaticUsers, err := patchExistingHysteriaConfig(raw, r.input.TrafficStatsListen, statsSecret)
		if err != nil {
			r.fail("Writing Hysteria2 config", err)
			return "", r.safeError(err)
		}
		config = patched
		if detectedPort > 0 {
			r.input.HysteriaPort = detectedPort
		}
		if hasStaticUsers {
			r.logs = append(r.logs, "Existing Hysteria2 static users were detected. LazGate Agent will become the auth source; recreate users in LazGate if needed.")
		}
	}
	script := fmt.Sprintf(`sudo install -d -m 755 /etc/hysteria
if sudo test -e /etc/hysteria/config.yaml; then
  sudo cp -a /etc/hysteria/config.yaml /etc/hysteria/config.yaml.lazgate-backup-$(date +%%Y%%m%%d%%H%%M%%S)
fi
sudo tee /etc/hysteria/config.yaml >/dev/null <<'EOF_CONFIG'
%s
EOF_CONFIG
if id hysteria >/dev/null 2>&1; then
  sudo chown hysteria:hysteria /etc/hysteria/config.yaml
  sudo chmod 600 /etc/hysteria/config.yaml
else
  sudo chmod 644 /etc/hysteria/config.yaml
fi`, config)
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail("Writing Hysteria2 config", err)
		return "", r.safeError(err)
	}
	r.ok("Writing Hysteria2 config", "Hysteria2 config written.")
	return serviceName, nil
}

func patchExistingHysteriaConfig(raw, statsListen, statsSecret string) (string, int, bool, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", 0, false, err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	hasStaticUsers := hasStaticAuthUsers(cfg["auth"])
	cfg["auth"] = map[string]any{
		"type": "http",
		"http": map[string]any{
			"url": "http://127.0.0.1:28262/auth",
		},
	}
	cfg["trafficStats"] = map[string]any{
		"listen": statsListen,
		"secret": statsSecret,
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", 0, false, err
	}
	return string(out), listenPort(cfg["listen"]), hasStaticUsers, nil
}

func hasStaticAuthUsers(auth any) bool {
	m, ok := auth.(map[string]any)
	if !ok {
		return false
	}
	if strings.EqualFold(fmt.Sprint(m["type"]), "password") || strings.EqualFold(fmt.Sprint(m["type"]), "userpass") {
		return true
	}
	if _, ok := m["password"]; ok {
		return true
	}
	if _, ok := m["userpass"]; ok {
		return true
	}
	return false
}

func listenPort(value any) int {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" {
		return 0
	}
	if strings.HasPrefix(raw, ":") {
		raw = strings.TrimPrefix(raw, ":")
	}
	if _, port, err := net.SplitHostPort(raw); err == nil {
		raw = port
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func (r *runner) installAgent(ctx context.Context, conn *ssh.Client, nodeID, serviceName, statsSecret string, certs mtlsFiles) error {
	r.start("Installing LazGate Agent")
	config := r.agentConfig(nodeID, serviceName, statsSecret)
	unit := lazgateAgentUnit()
	script := fmt.Sprintf(`set -eu
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) asset_arch="amd64" ;;
  aarch64|arm64) asset_arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 33 ;;
esac
sudo install -d -m 700 -o lazgate -g lazgate /etc/lazgate-agent /var/lib/lazgate-agent
tmp="$(mktemp -d)"
curl -fsSL -o "$tmp/lazgate-agent" %s/lazgate-agent-linux-"$asset_arch"
if curl -fsSL -o "$tmp/checksums.txt" %s/checksums.txt; then
  (cd "$tmp" && grep "lazgate-agent-linux-$asset_arch" checksums.txt | sed "s/lazgate-agent-linux-$asset_arch/lazgate-agent/" | sha256sum -c -)
fi
sudo install -m 755 "$tmp/lazgate-agent" /usr/local/bin/lazgate-agent
sudo tee /etc/lazgate-agent/config.yaml >/dev/null <<'EOF_AGENT_CONFIG'
%s
EOF_AGENT_CONFIG
sudo tee /etc/lazgate-agent/ca.crt >/dev/null <<'EOF_CA'
%s
EOF_CA
sudo tee /etc/lazgate-agent/server-ca.crt >/dev/null <<'EOF_SERVER_CA'
%s
EOF_SERVER_CA
sudo tee /etc/lazgate-agent/node.crt >/dev/null <<'EOF_CERT'
%s
EOF_CERT
sudo tee /etc/lazgate-agent/node.key >/dev/null <<'EOF_KEY'
%s
EOF_KEY
sudo chown -R lazgate:lazgate /etc/lazgate-agent /var/lib/lazgate-agent
sudo chmod 600 /etc/lazgate-agent/config.yaml /etc/lazgate-agent/node.key
sudo tee /etc/systemd/system/lazgate-agent.service >/dev/null <<'EOF_UNIT'
%s
EOF_UNIT
sudo systemctl daemon-reload
sudo systemctl enable lazgate-agent
sudo systemctl restart lazgate-agent`, shellQuote(r.input.AgentDownloadBase), shellQuote(r.input.AgentDownloadBase), config, certs.CAPEM, r.installer.agentServerCertPEM, certs.NodeCertPEM, certs.NodeKeyPEM, unit)
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail("Installing LazGate Agent", err)
		return r.safeError(err)
	}
	r.ok("Installing LazGate Agent", "LazGate Agent installed and started.")
	return nil
}

func (r *runner) startService(ctx context.Context, conn *ssh.Client, serviceName string) error {
	r.start("Starting Hysteria2 service")
	if _, err := runSSH(ctx, conn, "sudo systemctl daemon-reload && sudo systemctl enable "+shellQuote(serviceName)+" && sudo systemctl restart "+shellQuote(serviceName)); err != nil {
		if r.attach {
			_, _ = runSSH(ctx, conn, "latest=$(ls -t /etc/hysteria/config.yaml.lazgate-backup-* 2>/dev/null | head -n1); test -n \"$latest\" && sudo cp -a \"$latest\" /etc/hysteria/config.yaml && sudo systemctl restart "+shellQuote(serviceName))
		}
		r.fail("Starting Hysteria2 service", err)
		return r.safeError(err)
	}
	r.ok("Starting Hysteria2 service", "Hysteria2 service restarted.")
	return nil
}

func (r *runner) verify(ctx context.Context, conn *ssh.Client, serviceName string) error {
	r.start("Verifying service")
	script := "sudo systemctl is-active --quiet " + shellQuote(serviceName) + " && test -s /etc/hysteria/config.yaml && hysteria version >/dev/null 2>&1"
	if r.input.TrafficStatsEnabled {
		script += " && sudo grep -q " + shellQuote("listen: "+r.input.TrafficStatsListen) + " /etc/hysteria/config.yaml"
	}
	if _, err := runSSH(ctx, conn, script); err != nil {
		logs, _ := runSSH(ctx, conn, "sudo journalctl -u "+shellQuote(serviceName)+" -n 40 --no-pager")
		err = fmt.Errorf("%w; recent logs: %s", err, r.sanitize(logs))
		if r.input.ACMEEmail == "" && strings.Contains(strings.ToLower(err.Error()), "email") {
			err = errors.New("Hysteria2 failed during ACME setup. Provide an ACME email in advanced settings and retry")
		}
		r.fail("Verifying service", err)
		return r.safeError(err)
	}
	r.ok("Verifying service", "Service is active and config verified.")
	return nil
}

func (r *runner) hysteriaConfig(obfsPassword, statsSecret string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "listen: :%d\n", r.input.HysteriaPort)
	b.WriteString("acme:\n")
	b.WriteString("  domains:\n")
	fmt.Fprintf(&b, "    - %s\n", r.input.PublicDomain)
	if r.input.ACMEEmail != "" {
		fmt.Fprintf(&b, "  email: %s\n", r.input.ACMEEmail)
	}
	b.WriteString("auth:\n")
	b.WriteString("  type: http\n")
	b.WriteString("  http:\n")
	b.WriteString("    url: http://127.0.0.1:28262/auth\n")
	if r.input.ObfsEnabled {
		b.WriteString("obfs:\n")
		fmt.Fprintf(&b, "  type: %s\n", r.input.ObfsType)
		if r.input.ObfsType == "salamander" {
			b.WriteString("  salamander:\n")
			fmt.Fprintf(&b, "    password: %q\n", obfsPassword)
		}
	}
	b.WriteString("masquerade:\n")
	b.WriteString("  type: proxy\n")
	b.WriteString("  proxy:\n")
	fmt.Fprintf(&b, "    url: %s\n", r.input.MasqueradeURL)
	b.WriteString("    rewriteHost: true\n")
	if r.input.TrafficStatsEnabled {
		b.WriteString("trafficStats:\n")
		fmt.Fprintf(&b, "  listen: %s\n", r.input.TrafficStatsListen)
		fmt.Fprintf(&b, "  secret: %q\n", statsSecret)
	}
	return b.String()
}

func detectService(ctx context.Context, conn *ssh.Client) (string, error) {
	for _, name := range []string{"hysteria-server", "hysteria"} {
		if _, err := runSSH(ctx, conn, "systemctl cat "+shellQuote(name)+" >/dev/null 2>&1"); err == nil {
			return name, nil
		}
	}
	return "hysteria-server", nil
}

func (r *runner) verifyDomainResolves() error {
	ips, err := net.LookupHost(r.input.PublicDomain)
	if err != nil {
		return fmt.Errorf("public domain does not resolve: %w", err)
	}
	target := net.ParseIP(r.input.SSHHost)
	if target == nil {
		return nil
	}
	for _, ip := range ips {
		if net.ParseIP(ip).Equal(target) {
			return nil
		}
	}
	return fmt.Errorf("public domain resolves to %s, not %s", strings.Join(ips, ", "), target)
}

func generatedDomainForHost(host string) string {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return strings.TrimSpace(host)
	}
	return "h2." + strings.ReplaceAll(ip.String(), ".", "-") + ".sslip.io"
}

type sshConfig struct {
	Host       string
	Port       int
	User       string
	Password   string
	PrivateKey string
}

func dialSSH(ctx context.Context, cfg sshConfig) (*ssh.Client, error) {
	auth := []ssh.AuthMethod{}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	clientConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	type result struct {
		client *ssh.Client
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		client, err := ssh.Dial("tcp", cfg.Host+":"+strconv.Itoa(port), clientConfig)
		ch <- result{client: client, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.client, res.err
	}
}

func runSSH(ctx context.Context, conn *ssh.Client, script string) (string, error) {
	session, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run("sh -lc " + shellQuote(script))
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(msg))
	}
	return stdout.String(), nil
}

func generateSSHKey() (string, string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "lazgate generated key")
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(block)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

type mtlsFiles struct {
	CAPEM       string
	NodeCertPEM string
	NodeKeyPEM  string
}

func generateMTLSFiles(nodeID string) (mtlsFiles, error) {
	_, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return mtlsFiles{}, err
	}
	_, nodePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return mtlsFiles{}, err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "LazGate Agent CA " + nodeID},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPriv.Public(), caPriv)
	if err != nil {
		return mtlsFiles{}, err
	}
	nodeTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + 1),
		Subject:      pkix.Name{CommonName: nodeID},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	nodeDER, err := x509.CreateCertificate(rand.Reader, nodeTemplate, caTemplate, nodePriv.Public(), caPriv)
	if err != nil {
		return mtlsFiles{}, err
	}
	caKeyRaw, err := x509.MarshalPKCS8PrivateKey(caPriv)
	if err != nil {
		return mtlsFiles{}, err
	}
	nodeKeyRaw, err := x509.MarshalPKCS8PrivateKey(nodePriv)
	if err != nil {
		return mtlsFiles{}, err
	}
	_ = caKeyRaw
	return mtlsFiles{
		CAPEM:       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		NodeCertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: nodeDER})),
		NodeKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: nodeKeyRaw})),
	}, nil
}

func (r *runner) agentConfig(nodeID, serviceName, statsSecret string) string {
	caLine := ""
	if r.installer.agentServerCertPEM != "" {
		caLine = `  ca_file: "/etc/lazgate-agent/server-ca.crt"` + "\n"
	}
	return fmt.Sprintf(`node_id: %q
server_url: %q
agent_grpc_target: %q
state_path: "/var/lib/lazgate-agent/state.db"
transport_path: "/var/lib/lazgate-agent/transport.db"
mtls:
%s
  cert_file: "/etc/lazgate-agent/node.crt"
  key_file: "/etc/lazgate-agent/node.key"
hysteria2:
  auth_listen: "127.0.0.1:28262"
  stats_url: %q
  stats_secret: %q
  service_name: %q
  config_path: "/etc/hysteria/config.yaml"
sync:
  auth_sync_interval_seconds: 30
  traffic_collect_interval_seconds: 60
  online_collect_interval_seconds: 30
  heartbeat_interval_seconds: 30
  reconnect_min_backoff_seconds: 1
  reconnect_max_backoff_seconds: 60
  usage_queue_max_bytes: 1073741824
  usage_queue_max_age_days: 30
quota:
  default_guard_overage_bytes: 104857600
`, nodeID, r.input.ServerURL, r.input.AgentGRPCTarget, caLine, "http://"+r.input.TrafficStatsListen, statsSecret, serviceName)
}

func lazgateAgentUnit() string {
	return `[Unit]
Description=LazGate Agent
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/lazgate-agent --config /etc/lazgate-agent/config.yaml
Restart=always
RestartSec=5
User=lazgate
Group=lazgate
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/lazgate-agent /etc/hysteria
LockPersonality=true

[Install]
WantedBy=multi-user.target
`
}

func randomSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + fmt.Sprintf("%x", raw[:]), nil
}

func validDomain(domain string) bool {
	if len(domain) > 253 || strings.ContainsAny(domain, "/:@ ") {
		return false
	}
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 63 {
			return false
		}
	}
	return true
}

func (r *runner) start(name string) {
	for i := range r.steps {
		if r.steps[i].Name == name {
			r.steps[i].Status = StepRunning
			r.steps[i].StartedAt = time.Now().UTC()
			r.emit(r.steps[i])
			return
		}
	}
}

func (r *runner) ok(name, message string) {
	r.finish(name, StepOK, message)
}

func (r *runner) fail(name string, err error) {
	r.finish(name, StepFailed, r.sanitize(err.Error()))
}

func (r *runner) finish(name string, status StepStatus, message string) {
	for i := range r.steps {
		if r.steps[i].Name == name {
			r.steps[i].Status = status
			r.steps[i].Message = r.sanitize(message)
			r.steps[i].EndedAt = time.Now().UTC()
			r.logs = append(r.logs, time.Now().UTC().Format(time.RFC3339)+" "+name+": "+r.steps[i].Message)
			r.emit(r.steps[i])
			return
		}
	}
}

func (r *runner) emit(step Step) {
	if r.progress != nil {
		r.progress(step)
	}
}

func (r *runner) stepSucceeded(name string) bool {
	for _, step := range r.steps {
		if step.Name == name {
			return step.Status == StepOK
		}
	}
	return false
}

func (r *runner) safeError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(r.sanitize(err.Error()))
}

func (r *runner) sanitize(value string) string {
	value = strings.TrimSpace(value)
	for _, secret := range r.secrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	if r.input.BootstrapPassword != "" {
		value = strings.ReplaceAll(value, r.input.BootstrapPassword, "[redacted]")
	}
	return value
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
