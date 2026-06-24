package provisioning

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"

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

	"encoding/json"
	"laz/internal/protocol/hy2config"
	"laz/internal/protocol/hy2files"
	"laz/internal/server/model"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Store interface {
	CreateNodeWithInstanceAndAgentInstallation(model.Node, model.NodeInstance, model.AgentInstallation) (model.Node, model.NodeInstance, model.AgentInstallation, error)
	CreateHysteria2NodeConfig(model.Hysteria2NodeConfig) (model.Hysteria2NodeConfig, error)
	GetNodeRuntime(agentInstallationID string) (model.NodeRuntime, error)
	GetHysteria2NodeConfig(string) (model.Hysteria2NodeConfig, error)
}

type Installer struct {
	store              Store
	agentServerCertPEM string
	awgExecute         func(context.Context, AWGState, string) (string, error)
}

func New(st Store) *Installer {
	return &Installer{store: st}
}

func (i *Installer) SetAgentServerCertPEM(pem string) {
	i.agentServerCertPEM = strings.TrimSpace(pem)
}

type Hysteria2Request struct {
	FileSetID          string            `json:"file_set_id"`
	Settings           *hy2config.Config `json:"settings,omitempty"`
	SSHHost            string            `json:"ssh_host"`
	SSHPort            int               `json:"ssh_port"`
	BootstrapUser      string            `json:"bootstrap_user"`
	BootstrapPassword  string            `json:"bootstrap_password"`
	NodeName           string            `json:"node_name"`
	Region             string            `json:"region"`
	PublicDomain       string            `json:"public_domain"`
	HysteriaPort       int               `json:"hysteria_port"`
	MasqueradeURL      string            `json:"masquerade_url"`
	InstallVersion     string            `json:"install_version"`
	ACMEEmail          string            `json:"acme_email"`
	ObfsEnabled        bool              `json:"obfs_enabled"`
	ObfsType           string            `json:"obfs_type"`
	TrafficStatsListen string            `json:"traffic_stats_listen"`
	Progress           func(Step)        `json:"-"`
}

type Hysteria2Command struct {
	Request           Hysteria2Request
	ServerURL         string
	AgentGRPCTarget   string
	AgentDownloadBase string
}

type Hysteria2BootstrapInput struct {
	SSHHost           string
	SSHPort           int
	BootstrapUser     string
	BootstrapPassword string
	PublicDomain      string
	HysteriaPort      int
}

type Hysteria2InstallInput struct {
	FileSetID           string            `json:"file_set_id"`
	Settings            *hy2config.Config `json:"settings,omitempty"`
	SSHHost             string            `json:"ssh_host"`
	SSHPort             int               `json:"ssh_port"`
	NodeName            string            `json:"node_name"`
	Region              string            `json:"region"`
	PublicDomain        string            `json:"public_domain"`
	HysteriaPort        int               `json:"hysteria_port"`
	MasqueradeURL       string            `json:"masquerade_url"`
	InstallVersion      string            `json:"install_version"`
	ACMEEmail           string            `json:"acme_email"`
	ObfsEnabled         bool              `json:"obfs_enabled"`
	ObfsType            string            `json:"obfs_type"`
	TrafficStatsEnabled bool              `json:"traffic_stats_enabled"`
	TrafficStatsListen  string            `json:"traffic_stats_listen"`
	ServerURL           string            `json:"server_url"`
	AgentGRPCTarget     string            `json:"agent_grpc_target"`
	AgentDownloadBase   string            `json:"agent_download_base"`
}

type StepStatus string

const (
	ModeInstall = "install"
	ModeAttach  = "attach"

	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepOK      StepStatus = "ok"
	StepFailed  StepStatus = "failed"
)

type Step struct {
	Type      string     `json:"type"`
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
	input     Hysteria2InstallInput
	attach    bool
	steps     []Step
	logs      []string
	secrets   []string
	progress  func(Step)
}

const (
	h2AgentUser       = "h2-laz-agent"
	h2AgentBinaryName = "lazgate-hysteria-agent"
)

func (i *Installer) InstallHysteria2(ctx context.Context, command Hysteria2Command) (Result, error) {
	if err := i.validateAgentServerTrustMaterial(); err != nil {
		return Result{RetrySafe: true}, err
	}
	state, err := i.BootstrapHysteriaInstall(ctx, ModeInstall, command)
	if err != nil {
		return Result{Steps: state.Steps, Logs: state.Logs, RetrySafe: true}, err
	}
	return i.runRemainingInstall(ctx, state)
}

func (i *Installer) AttachHysteria2(ctx context.Context, command Hysteria2Command) (Result, error) {
	if err := i.validateAgentServerTrustMaterial(); err != nil {
		return Result{RetrySafe: true}, err
	}
	state, err := i.BootstrapHysteriaInstall(ctx, ModeAttach, command)
	if err != nil {
		return Result{Steps: state.Steps, Logs: state.Logs, RetrySafe: true}, err
	}
	return i.runRemainingInstall(ctx, state)
}

func (i *Installer) BootstrapHysteriaInstall(ctx context.Context, mode string, command Hysteria2Command) (InstallState, error) {
	if err := i.validateAgentServerTrustMaterial(); err != nil {
		return InstallState{}, err
	}
	input := command.Request
	state, err := InitialInstallState(mode, command)
	if err != nil {
		return state, err
	}
	bootstrap := input.bootstrapInput()
	r := &runner{installer: i, input: state.Input, attach: mode == "attach", steps: state.Steps, logs: state.Logs, progress: input.Progress}
	r.secrets = []string{bootstrap.BootstrapPassword, state.ObfsPassword, state.StatsSecret, state.PrivateKey, state.NodeKeyPEM}

	r.start(StepTypeConnect)
	if err := withBootstrap(ctx, bootstrap, func(conn *ssh.Client) error { return nil }); err != nil {
		r.fail(StepTypeConnect, err)
		state.Steps = r.steps
		state.Logs = r.logs
		return state, r.safeError(err)
	}
	r.ok(StepTypeConnect, "SSH-подключение установлено.")

	if err := withBootstrap(ctx, bootstrap, func(conn *ssh.Client) error { return r.preflight(ctx, conn) }); err != nil {
		state.Steps = r.steps
		state.Logs = r.logs
		return state, err
	}

	r.start(StepTypeCreateUser)
	if err := withBootstrap(ctx, bootstrap, func(conn *ssh.Client) error { return r.createUser(ctx, conn, state.PublicKey) }); err != nil {
		r.fail(StepTypeCreateUser, err)
		state.Steps = r.steps
		state.Logs = r.logs
		return state, err
	}
	if err := withLazgate(ctx, state, func(conn *ssh.Client) error { return nil }); err != nil {
		r.fail(StepTypeCreateUser, err)
		state.Steps = r.steps
		state.Logs = r.logs
		return state, r.safeError(err)
	}
	r.ok(StepTypeCreateUser, "SSH-пользователь готов.")

	state.Steps = r.steps
	state.Logs = r.logs
	return state, nil
}

func (i *Installer) validateAgentServerTrustMaterial() error {
	const requiredMessage = "agent gRPC server CA material is required for provisioning"
	certPEM := strings.TrimSpace(i.agentServerCertPEM)
	if certPEM == "" {
		return errors.New(requiredMessage)
	}

	block, rest := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return errors.New("agent gRPC server CA material must contain exactly one PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse agent gRPC server CA certificate: %w", err)
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return errors.New("agent gRPC server CA certificate is not currently valid")
	}
	if cert.IsCA {
		return errors.New("agent gRPC server CA certificate must be a non-CA TLS server certificate")
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return errors.New("agent gRPC server CA certificate must permit digital signatures")
	}
	if !containsServerAuthEKU(cert.ExtKeyUsage) {
		return errors.New("agent gRPC server CA certificate must explicitly permit TLS server authentication")
	}
	return nil
}

func containsServerAuthEKU(usages []x509.ExtKeyUsage) bool {
	for _, usage := range usages {
		if usage == x509.ExtKeyUsageServerAuth {
			return true
		}
	}
	return false
}

func (i *Installer) runRemainingInstall(ctx context.Context, state InstallState) (Result, error) {
	for stepType := StepTypeInstallDetect; stepType != ""; stepType = remainingNextStep(stepType) {
		result, err := i.ExecuteHysteriaStep(ctx, stepType, state)
		state = result.State
		if err != nil {
			return Result{Steps: state.Steps, Logs: state.Logs, RetrySafe: findStep(state.Steps, StepTypeWriteConfig).Status != StepOK}, err
		}
		if result.Done {
			return Result{
				Node:            state.Node,
				PublicDomain:    state.PublicDomain,
				HysteriaPort:    state.Input.HysteriaPort,
				ServiceName:     state.ServiceName,
				Steps:           state.Steps,
				Logs:            state.Logs,
				GeneratedDomain: state.GeneratedDomain,
			}, nil
		}
	}
	return Result{}, errors.New("hysteria install did not reach done step")
}

func remainingNextStep(stepType string) string {
	switch stepType {
	case StepTypeInstallDetect:
		return StepTypeWriteConfig
	case StepTypeWriteConfig:
		return StepTypeRegisterNode
	case StepTypeRegisterNode:
		return StepTypeInstallAgent
	case StepTypeInstallAgent:
		return StepTypeStartService
	case StepTypeStartService:
		return StepTypeVerify
	case StepTypeVerify:
		return StepTypeWaitAgent
	case StepTypeWaitAgent:
		return StepTypeDone
	default:
		return ""
	}
}

func normalize(input Hysteria2Request) Hysteria2Request {
	if input.Settings != nil {
		copy := *input.Settings
		input.Settings = &copy
		if input.HysteriaPort == 0 {
			input.HysteriaPort = copy.ListenPort
		}
		input.Settings.ListenPort = input.HysteriaPort
		input.ObfsEnabled = copy.Obfs.Type != "off"
		input.ObfsType = copy.Obfs.Type
	}
	input.SSHHost = strings.TrimSpace(input.SSHHost)
	input.BootstrapUser = strings.TrimSpace(input.BootstrapUser)
	input.NodeName = strings.TrimSpace(input.NodeName)
	if ip, err := normalizeServerIP(input.SSHHost); err == nil {
		input.SSHHost = ip
	}
	if region, err := normalizeServerRegion(input.Region); err == nil {
		input.Region = region
	}
	input.PublicDomain = strings.TrimSpace(input.PublicDomain)
	input.MasqueradeURL = strings.TrimSpace(input.MasqueradeURL)
	input.InstallVersion = strings.TrimSpace(input.InstallVersion)
	input.ACMEEmail = strings.TrimSpace(input.ACMEEmail)
	input.ObfsType = strings.TrimSpace(input.ObfsType)
	input.TrafficStatsListen = strings.TrimSpace(input.TrafficStatsListen)
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
	return input
}

func normalizeCommand(command Hysteria2Command) Hysteria2Command {
	command.Request = normalize(command.Request)
	command.ServerURL = strings.TrimRight(strings.TrimSpace(command.ServerURL), "/")
	command.AgentGRPCTarget = strings.TrimSpace(command.AgentGRPCTarget)
	command.AgentDownloadBase = strings.TrimRight(strings.TrimSpace(command.AgentDownloadBase), "/")
	return command
}

func (input Hysteria2Request) bootstrapInput() Hysteria2BootstrapInput {
	return Hysteria2BootstrapInput{
		SSHHost:           input.SSHHost,
		SSHPort:           input.SSHPort,
		BootstrapUser:     input.BootstrapUser,
		BootstrapPassword: input.BootstrapPassword,
		PublicDomain:      input.PublicDomain,
		HysteriaPort:      input.HysteriaPort,
	}
}

func (command Hysteria2Command) installInput() Hysteria2InstallInput {
	input := command.Request
	return Hysteria2InstallInput{
		FileSetID:           input.FileSetID,
		Settings:            input.Settings,
		SSHHost:             input.SSHHost,
		SSHPort:             input.SSHPort,
		NodeName:            input.NodeName,
		Region:              input.Region,
		PublicDomain:        input.PublicDomain,
		HysteriaPort:        input.HysteriaPort,
		MasqueradeURL:       input.MasqueradeURL,
		InstallVersion:      input.InstallVersion,
		ACMEEmail:           input.ACMEEmail,
		ObfsEnabled:         input.ObfsEnabled,
		ObfsType:            input.ObfsType,
		TrafficStatsEnabled: true,
		TrafficStatsListen:  input.TrafficStatsListen,
		ServerURL:           command.ServerURL,
		AgentGRPCTarget:     command.AgentGRPCTarget,
		AgentDownloadBase:   command.AgentDownloadBase,
	}
}

func (r *runner) initSteps() {
	r.steps = InitialSteps(r.attach)
}

func InitialSteps(attach bool) []Step {
	stepTypes := []string{
		StepTypeConnect,
		StepTypeCheckSystem,
		StepTypeCreateUser,
		StepTypeInstallDetect,
		StepTypeWriteConfig,
		StepTypeRegisterNode,
		StepTypeInstallAgent,
		StepTypeStartService,
		StepTypeVerify,
		StepTypeWaitAgent,
		StepTypeDone,
	}
	steps := make([]Step, 0, len(stepTypes))
	for _, stepType := range stepTypes {
		steps = append(steps, Step{Type: stepType, Name: stepDisplayName(stepType, attach), Status: StepPending})
	}
	return steps
}

func stepDisplayName(stepType string, attach bool) string {
	switch stepType {
	case StepTypeConnect:
		return "Подключение к VPS"
	case StepTypeCheckSystem:
		return "Проверка системы"
	case StepTypeCreateUser:
		return "Создание SSH-пользователя Hysteria agent"
	case StepTypeInstallDetect:
		if attach {
			return "Поиск существующей Hysteria2"
		}
		return "Установка Hysteria2"
	case StepTypeWriteConfig:
		return "Запись конфигурации Hysteria2"
	case StepTypeRegisterNode:
		return "Регистрация сервера"
	case StepTypeInstallAgent:
		return "Установка LazGate Agent"
	case StepTypeStartService:
		return "Запуск сервиса Hysteria2"
	case StepTypeVerify:
		return "Проверка сервиса"
	case StepTypeWaitAgent:
		return "Ожидание LazGate Agent"
	case StepTypeDone:
		return "Готово"
	default:
		return stepType
	}
}

func (r *runner) waitAgent(ctx context.Context, installationID, nodeID string) error {
	r.start(StepTypeWaitAgent)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		runtime, err := r.installer.store.GetNodeRuntime(installationID)
		if err == nil && runtime.AgentStatus == "online" && !runtime.LastHeartbeatAt.IsZero() {
			config, e := r.installer.store.GetHysteria2NodeConfig(nodeID)
			if e != nil {
				return e
			}
			if config.ConfigErrorID != "" {
				return fmt.Errorf("Hysteria configuration apply failed: %s", config.ConfigErrorID)
			}
			if config.SettingsJSON != "" && config.AppliedConfigVersionMS < config.ConfigVersionMS {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
				continue
			}
			r.ok(StepTypeWaitAgent, "Agent подключился, heartbeat получен.")
			return nil
		}
		select {
		case <-ctx.Done():
			r.fail(StepTypeWaitAgent, ctx.Err())
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	err := errors.New("агент не подключился обратно к LazGate вовремя")
	r.fail(StepTypeWaitAgent, err)
	return r.safeError(err)
}

func validateCommand(command Hysteria2Command) error {
	if err := validateRequest(command.Request); err != nil {
		return err
	}
	return validateInstallInput(command.installInput())
}

func validateRequest(input Hysteria2Request) error {
	if input.SSHHost == "" || input.BootstrapPassword == "" || input.NodeName == "" {
		return errors.New("ssh host, bootstrap password and node name are required")
	}
	return nil
}

func validateInstallInput(input Hysteria2InstallInput) error {
	if input.Settings != nil {
		if err := hy2config.Validate(*input.Settings); err != nil {
			return err
		}
	}
	if _, err := normalizeServerIP(input.SSHHost); err != nil {
		return err
	}
	if _, err := normalizeServerRegion(input.Region); err != nil {
		return err
	}
	if input.SSHHost == "" || input.NodeName == "" {
		return errors.New("ssh host and node name are required")
	}
	if input.ServerURL == "" {
		return errors.New("server_url is required")
	}
	if input.AgentGRPCTarget != "" {
		if _, _, err := net.SplitHostPort(input.AgentGRPCTarget); err != nil {
			return errors.New("agent_grpc_target must be host:port")
		}
	}
	if input.SSHPort < 1 || input.SSHPort > 65535 || input.HysteriaPort < 1 || input.HysteriaPort > 65535 {
		return errors.New("ports must be between 1 and 65535")
	}
	if input.ACMEEmail != "" {
		if _, err := mail.ParseAddress(input.ACMEEmail); err != nil {
			return errors.New("invalid ACME email")
		}
	}
	if _, err := url.ParseRequestURI(input.MasqueradeURL); err != nil {
		return errors.New("invalid masquerade URL")
	}
	if input.PublicDomain != "" && !validDomain(input.PublicDomain) {
		return errors.New("invalid public domain")
	}
	if !strings.Contains(input.TrafficStatsListen, "127.0.0.1:") {
		return errors.New("traffic stats listen address must stay on 127.0.0.1")
	}
	return nil
}

func (r *runner) validate() error {
	if err := validateInstallInput(r.input); err != nil {
		return err
	}
	return nil
}

func (r *runner) preflight(ctx context.Context, conn *ssh.Client) error {
	r.start(StepTypeCheckSystem)
	installGuards := `! command -v hysteria >/dev/null
test ! -e /etc/hysteria/config.yaml`
	agentGuards := fmt.Sprintf(`if id %[1]s >/dev/null 2>&1; then exit 24; fi
test ! -e /etc/sudoers.d/%[1]s
test ! -e /etc/systemd/system/lazgate-agent.service
test ! -e /usr/local/bin/%[2]s
test ! -e /etc/lazgate-agent
test ! -e /var/lib/lazgate-agent`, h2AgentUser, h2AgentBinaryName)
	if r.attach {
		installGuards = `if ! command -v hysteria >/dev/null && ! systemctl cat hysteria-server >/dev/null 2>&1 && ! systemctl cat hysteria >/dev/null 2>&1; then
  echo "hysteria is not installed" >&2
  exit 22
fi`
		agentGuards = "true"
	}
	portGuard := fmt.Sprintf(`ss -lun | awk '{print $5}' | grep -Eq ':(%d)$' && exit 20 || true`, r.input.HysteriaPort)
	if r.attach {
		portGuard = "true"
	}
	script := fmt.Sprintf(`set -eu
id -u
test "$(id -u)" = "0"
test -d /run/systemd/system
command -v systemctl >/dev/null
command -v curl >/dev/null
command -v grep >/dev/null
command -v awk >/dev/null
command -v sed >/dev/null
command -v sha256sum >/dev/null
command -v mktemp >/dev/null
command -v sudo >/dev/null
command -v useradd >/dev/null
command -v install >/dev/null
command -v chmod >/dev/null
command -v chown >/dev/null
command -v tee >/dev/null
command -v ss >/dev/null
test -d /etc/sudoers.d
test -w /etc/systemd/system
test -w /usr/local/bin || test -w /usr/local
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64|aarch64|arm64) ;;
  *) exit 23 ;;
esac
. /etc/os-release
echo "$ID $VERSION_ID"
%s
%s
%s
ss -ltn | awk '{print $4}' | grep -Eq ':(80)$' && exit 21 || true`, installGuards, agentGuards, portGuard)
	if _, err := runSSH(ctx, conn, script); err != nil {
		if strings.Contains(err.Error(), "Process exited with status 20") {
			err = errors.New("выбранный UDP порт Hysteria2 уже занят")
		} else if strings.Contains(err.Error(), "Process exited with status 21") {
			err = errors.New("TCP порт 80 уже занят; он нужен для ACME HTTP challenge")
		} else if strings.Contains(err.Error(), "Process exited with status 22") {
			err = errors.New("существующая установка Hysteria2 не найдена")
		} else if strings.Contains(err.Error(), "Process exited with status 23") {
			err = errors.New("архитектура CPU не поддерживается LazGate Agent; поддерживаются amd64 и arm64")
		} else if strings.Contains(err.Error(), "Process exited with status 24") {
			err = errors.New("пользователь LazGate Agent уже существует; удалите предыдущую установку агента перед добавлением VPS")
		}
		r.fail(StepTypeCheckSystem, err)
		return r.safeError(err)
	}
	if err := r.verifyDomainResolves(); err != nil {
		r.fail(StepTypeCheckSystem, err)
		return r.safeError(err)
	}
	r.ok(StepTypeCheckSystem, "Проверки Linux/systemd/curl/agent пройдены.")
	return nil
}

func (r *runner) createUser(ctx context.Context, conn *ssh.Client, publicKey string) error {
	r.start(StepTypeCreateUser)
	script := fmt.Sprintf(`set -eu
id %[1]s >/dev/null 2>&1 || useradd --system --create-home --home-dir /home/%[1]s --shell /bin/bash %[1]s
install -d -m 700 -o %[1]s -g %[1]s /home/%[1]s/.ssh
cat > /home/%[1]s/.ssh/authorized_keys <<'EOF_KEY'
%[2]s
EOF_KEY
chown %[1]s:%[1]s /home/%[1]s/.ssh/authorized_keys
chmod 600 /home/%[1]s/.ssh/authorized_keys
cat > /etc/sudoers.d/h2-laz-agent <<'EOF_SUDO'
%[1]s ALL=(root) NOPASSWD:ALL
EOF_SUDO
chmod 440 /etc/sudoers.d/h2-laz-agent`, h2AgentUser, publicKey)
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail(StepTypeCreateUser, err)
		return r.safeError(err)
	}
	return nil
}

func (r *runner) installOrDetectHysteria(ctx context.Context, conn *ssh.Client) error {
	if r.attach {
		r.start(StepTypeInstallDetect)
		if _, err := detectService(ctx, conn); err != nil {
			r.fail(StepTypeInstallDetect, err)
			return r.safeError(err)
		}
		if _, err := runSSH(ctx, conn, `command -v hysteria >/dev/null 2>&1 || systemctl cat hysteria-server >/dev/null 2>&1 || systemctl cat hysteria >/dev/null 2>&1`); err != nil {
			r.fail(StepTypeInstallDetect, err)
			return r.safeError(errors.New("существующая установка Hysteria2 не найдена"))
		}
		r.ok(StepTypeInstallDetect, "Существующий сервис Hysteria2 найден.")
		return nil
	}
	r.start(StepTypeInstallDetect)
	if _, err := runSSH(ctx, conn, `sudo bash -lc 'bash <(curl -fsSL https://get.hy2.sh/)'`); err != nil {
		r.fail(StepTypeInstallDetect, err)
		return r.safeError(err)
	}
	r.ok(StepTypeInstallDetect, "Официальный install script Hysteria2 завершён.")
	return nil
}

func (r *runner) writeConfig(ctx context.Context, conn *ssh.Client, obfsPassword, statsSecret string) (string, error) {
	r.start(StepTypeWriteConfig)
	serviceName, err := detectService(ctx, conn)
	if err != nil {
		r.fail(StepTypeWriteConfig, err)
		return "", r.safeError(err)
	}
	config, err := r.hysteriaConfig(obfsPassword, statsSecret)
	if err != nil {
		return "", err
	}
	if r.attach {
		raw, err := runSSH(ctx, conn, "sudo cat "+shellQuote(hysteriaConfigPath))
		if err != nil {
			r.fail(StepTypeWriteConfig, err)
			return "", r.safeError(err)
		}
		patched, detectedPort, hasStaticUsers, err := patchExistingHysteriaConfig(raw, r.input.TrafficStatsListen, statsSecret)
		if err != nil {
			r.fail(StepTypeWriteConfig, err)
			return "", r.safeError(err)
		}
		config = patched
		if detectedPort > 0 {
			r.input.HysteriaPort = detectedPort
		}
		if hasStaticUsers {
			r.logs = append(r.logs, "Найдены статические пользователи Hysteria2. LazGate Agent станет источником авторизации; при необходимости пересоздайте пользователей в LazGate.")
		}
	}
	backupScript := ""
	if r.attach {
		backupScript = fmt.Sprintf(`
if sudo test -e %[1]s; then
  echo "Backup конфигурации LazGate уже существует; сначала отключите предыдущий attach перед повторным подключением VPS" >&2
  exit 41
fi
sudo test -f %[2]s
sudo cp -a %[2]s %[1]s
sudo chmod 600 %[1]s
`, shellQuote(hysteriaAttachBackupPath), shellQuote(hysteriaConfigPath))
	}
	script := fmt.Sprintf(`sudo install -d -m 755 /etc/hysteria%[1]s
sudo tee %[2]s >/dev/null <<'EOF_CONFIG'
%[3]s
EOF_CONFIG
if id hysteria >/dev/null 2>&1; then
  sudo chown hysteria:hysteria %[2]s
  sudo chmod 600 %[2]s
else
  sudo chmod 644 %[2]s
fi`, backupScript, shellQuote(hysteriaConfigPath), config)
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail(StepTypeWriteConfig, err)
		return "", r.safeError(err)
	}
	r.ok(StepTypeWriteConfig, "Конфигурация Hysteria2 записана.")
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

func restoreAttachManagedHysteriaConfig(currentRaw, backupRaw string) (string, error) {
	var current map[string]any
	if err := yaml.Unmarshal([]byte(currentRaw), &current); err != nil {
		return "", err
	}
	if current == nil {
		current = map[string]any{}
	}
	var backup map[string]any
	if err := yaml.Unmarshal([]byte(backupRaw), &backup); err != nil {
		return "", err
	}
	restoreConfigKey(current, backup, "auth")
	restoreConfigKey(current, backup, "trafficStats")
	out, err := yaml.Marshal(current)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func restoreConfigKey(current, backup map[string]any, key string) {
	value, ok := backup[key]
	if !ok {
		delete(current, key)
		return
	}
	current[key] = value
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

func (r *runner) installAgent(ctx context.Context, conn *ssh.Client, nodeID, installationID, agentType, serviceName, statsSecret string, certs mtlsFiles) error {
	r.start(StepTypeInstallAgent)
	if err := r.installer.validateAgentServerTrustMaterial(); err != nil {
		return err
	}
	script := renderHysteriaAgentInstallScript(agentInstallScriptInput{
		AgentDownloadBase: r.input.AgentDownloadBase,
		AgentConfig:       r.agentConfig(nodeID, installationID, agentType, serviceName, statsSecret),
		CAPEM:             certs.CAPEM,
		ServerCAPEM:       r.installer.agentServerCertPEM,
		NodeCertPEM:       certs.NodeCertPEM,
		NodeKeyPEM:        certs.NodeKeyPEM,
	})
	if _, err := runSSH(ctx, conn, script); err != nil {
		r.fail(StepTypeInstallAgent, err)
		return r.safeError(err)
	}
	r.ok(StepTypeInstallAgent, "LazGate Agent установлен и запущен.")
	return nil
}

type agentInstallScriptInput struct {
	AgentDownloadBase string
	AgentConfig       string
	CAPEM             string
	ServerCAPEM       string
	NodeCertPEM       string
	NodeKeyPEM        string
}

func renderHysteriaAgentInstallScript(input agentInstallScriptInput) string {
	return "set -eu\nsudo install -d -o root -g root -m 755 " + shellQuote(hy2files.ManagedDirectory) + "\n" + renderAgentInstallScript(input, h2AgentUser, h2AgentBinaryName, lazgateAgentUnit())
}

func renderAgentInstallScript(input agentInstallScriptInput, user, binary, unit string) string {
	return fmt.Sprintf(`set -eu
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) asset_arch="amd64" ;;
  aarch64|arm64) asset_arch="arm64" ;;
  *) echo "неподдерживаемая архитектура: $arch" >&2; exit 33 ;;
esac
sudo install -d -m 700 -o %[1]s -g %[1]s /etc/lazgate-agent /var/lib/lazgate-agent
tmp="$(mktemp -d)"
agent_asset="%[9]s-linux-$asset_arch"
curl -fsSL -o "$tmp/%[9]s" %[2]s/"$agent_asset"
curl -fsSL -o "$tmp/checksums.txt" %[2]s/checksums.txt
(cd "$tmp" && grep " $agent_asset$" checksums.txt | sed "s/$agent_asset/%[9]s/" | sha256sum -c -)
sudo install -m 755 "$tmp/%[9]s" /usr/local/bin/%[9]s
sudo tee /etc/lazgate-agent/config.yaml >/dev/null <<'EOF_AGENT_CONFIG'
%[3]s
EOF_AGENT_CONFIG
sudo tee /etc/lazgate-agent/ca.crt >/dev/null <<'EOF_CA'
%[4]s
EOF_CA
sudo tee /etc/lazgate-agent/server-ca.crt >/dev/null <<'EOF_SERVER_CA'
%[5]s
EOF_SERVER_CA
sudo tee /etc/lazgate-agent/node.crt >/dev/null <<'EOF_CERT'
%[6]s
EOF_CERT
sudo tee /etc/lazgate-agent/node.key >/dev/null <<'EOF_KEY'
%[7]s
EOF_KEY
sudo chown -R %[1]s:%[1]s /etc/lazgate-agent /var/lib/lazgate-agent
sudo chmod 600 /etc/lazgate-agent/config.yaml /etc/lazgate-agent/node.key
sudo tee /etc/systemd/system/lazgate-agent.service >/dev/null <<'EOF_UNIT'
%[8]s
EOF_UNIT
sudo systemctl daemon-reload
sudo systemctl enable lazgate-agent
sudo systemctl restart lazgate-agent`, user, shellQuote(input.AgentDownloadBase), input.AgentConfig, input.CAPEM, input.ServerCAPEM, input.NodeCertPEM, input.NodeKeyPEM, unit, binary)
}

func (r *runner) startService(ctx context.Context, conn *ssh.Client, serviceName string) error {
	r.start(StepTypeStartService)
	if _, err := runSSH(ctx, conn, "sudo systemctl daemon-reload && sudo systemctl enable "+shellQuote(serviceName)+" && sudo systemctl restart "+shellQuote(serviceName)); err != nil {
		if r.attach {
			_, _ = runSSH(ctx, conn, "sudo test -f "+shellQuote(hysteriaAttachBackupPath)+" && sudo cp -a "+shellQuote(hysteriaAttachBackupPath)+" "+shellQuote(hysteriaConfigPath)+" && sudo systemctl restart "+shellQuote(serviceName))
		}
		r.fail(StepTypeStartService, err)
		return r.safeError(err)
	}
	r.ok(StepTypeStartService, "Сервис Hysteria2 перезапущен.")
	return nil
}

func (r *runner) verify(ctx context.Context, conn *ssh.Client, serviceName string) error {
	r.start(StepTypeVerify)
	script := "sudo systemctl is-active --quiet " + shellQuote(serviceName) + " && test -s /etc/hysteria/config.yaml && hysteria version >/dev/null 2>&1"
	if r.input.TrafficStatsEnabled {
		script += " && sudo grep -q " + shellQuote("listen: "+r.input.TrafficStatsListen) + " /etc/hysteria/config.yaml"
	}
	if _, err := runSSH(ctx, conn, script); err != nil {
		logs, _ := runSSH(ctx, conn, "sudo journalctl -u "+shellQuote(serviceName)+" -n 40 --no-pager")
		err = fmt.Errorf("%w; recent logs: %s", err, r.sanitize(logs))
		if r.input.ACMEEmail == "" && strings.Contains(strings.ToLower(err.Error()), "email") {
			err = errors.New("Hysteria2 не смогла пройти ACME setup. Укажите ACME email в расширенных настройках и повторите попытку")
		}
		r.fail(StepTypeVerify, err)
		return r.safeError(err)
	}
	r.ok(StepTypeVerify, "Сервис активен, конфигурация проверена.")
	return nil
}

func (r *runner) hysteriaConfig(obfsPassword, statsSecret string) (string, error) {
	if r.input.Settings != nil {
		settings := *r.input.Settings
		settings.ListenPort = r.input.HysteriaPort
		base := map[string]any{"acme": map[string]any{"domains": []string{r.input.PublicDomain}}, "auth": map[string]any{"type": "http", "http": map[string]any{"url": "http://127.0.0.1:28262/auth"}}, "trafficStats": map[string]any{"listen": r.input.TrafficStatsListen, "secret": statsSecret}}
		result, err := settings.Runtime(base, r.input.PublicDomain)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
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
	return b.String(), nil
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
	session.Stdin = strings.NewReader(script)
	done := make(chan error, 1)
	go func() { done <- session.Run("sh -s") }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return "", ctx.Err()
	}
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

func generateMTLSFiles(installationID string) (mtlsFiles, error) {
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
		Subject:               pkix.Name{CommonName: "LazGate Agent CA " + installationID},
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
		Subject:      pkix.Name{CommonName: installationID},
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

func parseMTLSCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("invalid agent certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

func certificateFingerprint(cert *x509.Certificate) string {
	return fmt.Sprintf("%x", sha256.Sum256(cert.Raw))
}

func (r *runner) agentConfig(nodeID, installationID, agentType, serviceName, statsSecret string) string {
	return fmt.Sprintf(`node_id: %q
installation_id: %q
agent_type: %q
server_url: %q
agent_grpc_target: %q
state_path:
transport_path: "/var/lib/lazgate-agent/transport.db"
mtls:
  ca_file: "/etc/lazgate-agent/server-ca.crt"
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
  telemetry_collect_interval_seconds: 30
  heartbeat_interval_seconds: 30
  reconnect_min_backoff_seconds: 1
  reconnect_max_backoff_seconds: 300
`, nodeID, installationID, agentType, r.input.ServerURL, r.input.AgentGRPCTarget, "http://"+r.input.TrafficStatsListen, statsSecret, serviceName)
}

func lazgateAgentUnit() string {
	return `[Unit]
Description=LazGate Agent
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/` + h2AgentBinaryName + ` --config /etc/lazgate-agent/config.yaml
Restart=always
RestartSec=5
User=` + h2AgentUser + `
Group=` + h2AgentUser + `
# The agent invokes the installation's sudo rules for runtime/configuration changes.
NoNewPrivileges=false
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/lazgate-agent /etc/hysteria ` + hy2files.ManagedDirectory + `
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

func (r *runner) start(stepType string) {
	for i := range r.steps {
		if r.steps[i].Type == stepType {
			r.steps[i].Status = StepRunning
			r.steps[i].StartedAt = time.Now().UTC()
			r.emit(r.steps[i])
			return
		}
	}
}

func (r *runner) ok(stepType, message string) {
	r.finish(stepType, StepOK, message)
}

func (r *runner) fail(stepType string, err error) {
	r.finish(stepType, StepFailed, r.sanitize(err.Error()))
}

func (r *runner) finish(stepType string, status StepStatus, message string) {
	for i := range r.steps {
		if r.steps[i].Type == stepType {
			r.steps[i].Status = status
			r.steps[i].Message = r.sanitize(message)
			r.steps[i].EndedAt = time.Now().UTC()
			r.logs = append(r.logs, time.Now().UTC().Format(time.RFC3339)+" "+r.steps[i].Name+": "+r.steps[i].Message)
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

func (r *runner) stepSucceeded(stepType string) bool {
	for _, step := range r.steps {
		if step.Type == stepType {
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

	return value
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
