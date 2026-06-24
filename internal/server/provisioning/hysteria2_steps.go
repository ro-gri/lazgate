package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"laz/internal/protocol/hy2config"
	"net"
	"net/url"

	"laz/internal/server/model"
	storeutil "laz/internal/server/storage"

	"golang.org/x/crypto/ssh"
)

const (
	hysteriaConfigPath       = "/etc/hysteria/config.yaml"
	hysteriaAttachBackupPath = "/etc/hysteria/config.yaml.lazgate-backup"

	StepTypeConnect       = "hysteria.install.connect"
	StepTypeCheckSystem   = "hysteria.install.check_system"
	StepTypeCreateUser    = "hysteria.install.create_user"
	StepTypeInstallDetect = "hysteria.install.install_or_detect"
	StepTypeWriteConfig   = "hysteria.install.write_config"
	StepTypeInstallAgent  = "hysteria.install.install_agent"
	StepTypeStartService  = "hysteria.install.start_service"
	StepTypeVerify        = "hysteria.install.verify"
	StepTypeRegisterNode  = "hysteria.install.register_node"
	StepTypeWaitAgent     = "hysteria.install.wait_agent"
	StepTypeDone          = "hysteria.install.done"
)

type InstallState struct {
	Mode            string                `json:"mode"`
	Input           Hysteria2InstallInput `json:"input"`
	GeneratedDomain bool                  `json:"generated_domain"`
	NodeID          string                `json:"node_id"`
	NodeInstanceID  string                `json:"node_instance_id"`
	InstallationID  string                `json:"installation_id"`
	AgentType       string                `json:"agent_type"`
	PublicDomain    string                `json:"public_domain"`
	ServiceName     string                `json:"service_name"`
	ObfsPassword    string                `json:"obfs_password"`
	StatsSecret     string                `json:"stats_secret"`
	PrivateKey      string                `json:"private_key"`
	PublicKey       string                `json:"public_key"`
	CAPEM           string                `json:"ca_pem"`
	NodeCertPEM     string                `json:"node_cert_pem"`
	NodeKeyPEM      string                `json:"node_key_pem"`
	Node            model.Node            `json:"node"`
	Steps           []Step                `json:"steps"`
	Logs            []string              `json:"logs"`
}

type StepResult struct {
	State InstallState
	Step  Step
	Done  bool
}

func InitialInstallState(mode string, command Hysteria2Command) (InstallState, error) {
	command = normalizeCommand(command)
	if err := validateCommand(command); err != nil {
		return InstallState{}, err
	}
	input := command.Request
	installInput := command.installInput()
	generatedDomain := false
	if installInput.PublicDomain == "" {
		if ip := net.ParseIP(input.SSHHost); ip != nil && ip.To4() == nil {
			return InstallState{}, errors.New("public domain is required when installing through IPv6")
		}
		installInput.PublicDomain = generatedDomainForHost(input.SSHHost)
		generatedDomain = true
	}
	obfsPassword, err := randomSecret()
	if err != nil {
		return InstallState{}, err
	}
	if installInput.Settings != nil {
		obfsPassword = installInput.Settings.Obfs.Password
	}
	statsSecret, err := randomSecret()
	if err != nil {
		return InstallState{}, err
	}
	privateKey, publicKey, err := generateSSHKey()
	if err != nil {
		return InstallState{}, err
	}
	nodeID := NewNodeID()
	installationID := storeutil.NewID("ain")
	certs, err := generateMTLSFiles(installationID)
	if err != nil {
		return InstallState{}, err
	}
	return InstallState{
		Mode:            mode,
		Input:           installInput,
		GeneratedDomain: generatedDomain,
		NodeID:          nodeID,
		NodeInstanceID:  storeutil.NewID("nins"),
		InstallationID:  installationID,
		AgentType:       "hysteria2",
		PublicDomain:    installInput.PublicDomain,
		ObfsPassword:    obfsPassword,
		StatsSecret:     statsSecret,
		PrivateKey:      privateKey,
		PublicKey:       publicKey,
		CAPEM:           certs.CAPEM,
		NodeCertPEM:     certs.NodeCertPEM,
		NodeKeyPEM:      certs.NodeKeyPEM,
		Steps:           InitialSteps(mode == "attach"),
	}, nil
}

func NewNodeID() string {
	return storeutil.NewID("nod")
}

func (i *Installer) ExecuteHysteriaStep(ctx context.Context, stepType string, state InstallState) (StepResult, error) {
	state.Input.PublicDomain = state.PublicDomain
	state.Steps = normalizeInstallSteps(state.Steps, state.Mode == "attach")
	r := &runner{installer: i, input: state.Input, attach: state.Mode == "attach", steps: state.Steps, logs: state.Logs}
	r.secrets = []string{state.ObfsPassword, state.StatsSecret, state.PrivateKey, state.NodeKeyPEM}
	var err error
	switch stepType {
	case StepTypeConnect, StepTypeCheckSystem, StepTypeCreateUser:
		return StepResult{}, errors.New("bootstrap install steps must run synchronously before enqueue")
	case StepTypeInstallDetect:
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.installOrDetectHysteria(ctx, conn) })
	case StepTypeWriteConfig:
		err = withLazgate(ctx, state, func(conn *ssh.Client) error {
			serviceName, err := r.writeConfig(ctx, conn, state.ObfsPassword, state.StatsSecret)
			state.ServiceName = serviceName
			if err == nil && state.Input.Settings == nil {
				raw, e := runSSH(ctx, conn, "sudo cat "+shellQuote(hysteriaConfigPath))
				if e != nil {
					return e
				}
				var current map[string]any
				if e = yaml.Unmarshal([]byte(raw), &current); e != nil {
					return e
				}
				settings, e := hy2config.FromRuntime(current)
				if e != nil {
					return e
				}
				state.Input.Settings = &settings
				state.ObfsPassword = settings.Obfs.Password
				state.Input.ObfsEnabled = settings.Obfs.Type != "off"
				state.Input.ObfsType = settings.Obfs.Type
			}
			return err
		})
	case StepTypeInstallAgent:
		err = withLazgate(ctx, state, func(conn *ssh.Client) error {
			return r.installAgent(ctx, conn, state.NodeID, state.InstallationID, state.AgentType, state.ServiceName, state.StatsSecret, mtlsFiles{CAPEM: state.CAPEM, NodeCertPEM: state.NodeCertPEM, NodeKeyPEM: state.NodeKeyPEM})
		})
	case StepTypeStartService:
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.startService(ctx, conn, state.ServiceName) })
	case StepTypeVerify:
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.verify(ctx, conn, state.ServiceName) })
	case StepTypeRegisterNode:
		r.start(stepType)
		state.Node, err = i.registerNode(state)
		if err == nil {
			r.ok(stepType, "Сервер зарегистрирован в LazGate.")
		}
	case StepTypeWaitAgent:
		err = r.waitAgent(ctx, state.InstallationID, state.NodeID)
	case StepTypeDone:
		r.start(stepType)
		r.ok(stepType, "Hysteria2 VPS сервер установлен и зарегистрирован.")
	default:
		return StepResult{}, errors.New("неизвестный шаг установки Hysteria2")
	}
	if err != nil && findStep(r.steps, stepType).Status != StepFailed {
		r.fail(stepType, err)
	}
	state.Steps = r.steps
	state.Logs = r.logs
	if err != nil {
		return StepResult{State: state, Step: findStep(r.steps, stepType)}, err
	}
	return StepResult{State: state, Step: findStep(r.steps, stepType), Done: stepType == StepTypeDone}, nil
}

func (i *Installer) registerNode(state InstallState) (model.Node, error) {
	cert, err := parseMTLSCertificate(state.NodeCertPEM)
	if err != nil {
		return model.Node{}, err
	}
	node, _, _, err := i.store.CreateNodeWithInstanceAndAgentInstallation(model.Node{
		ID: state.NodeID, Name: state.Input.NodeName, Type: model.NodeTypeNativeHy2,
		Region: state.Input.Region,
	}, model.NodeInstance{
		ID: state.NodeInstanceID, NodeID: state.NodeID, Host: state.Input.SSHHost, SSHPort: state.Input.SSHPort, SSHUser: h2AgentUser, SSHKey: state.PrivateKey,
	}, model.AgentInstallation{
		ID:                           state.InstallationID,
		NodeInstanceID:               state.NodeInstanceID,
		AgentType:                    state.AgentType,
		CertificateFingerprintSHA256: certificateFingerprint(cert),
		CertificateSerial:            cert.SerialNumber.String(),
		CertificateNotBefore:         cert.NotBefore,
		CertificateNotAfter:          cert.NotAfter,
	})
	if err == nil {
		config := model.Hysteria2NodeConfig{NodeID: state.NodeID, EndpointHost: state.PublicDomain, EndpointPort: state.Input.HysteriaPort, ObfsEnabled: state.Input.ObfsEnabled, ObfsType: state.Input.ObfsType, ObfsPassword: state.ObfsPassword, MasqueradeURL: state.Input.MasqueradeURL}
		config.MasqueradeFileSetID = state.Input.FileSetID
		if state.Input.Settings != nil {
			raw, e := json.Marshal(state.Input.Settings)
			if e != nil {
				return node, e
			}
			config.SettingsJSON = string(raw)
			config.MasqueradeURL = ""
			if state.Input.Settings.Masquerade.Type == "proxy" {
				parsed, e := url.Parse(state.Input.Settings.Masquerade.URL)
				if e != nil {
					return node, e
				}
				parsed.User = nil
				config.MasqueradeURL = parsed.String()
			}
		}
		_, err = i.store.CreateHysteria2NodeConfig(config)
	}
	return node, err
}

func withBootstrap(ctx context.Context, input Hysteria2BootstrapInput, fn func(*ssh.Client) error) error {
	conn, err := dialSSH(ctx, sshConfig{Host: input.SSHHost, Port: input.SSHPort, User: input.BootstrapUser, Password: input.BootstrapPassword})
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

func withLazgate(ctx context.Context, state InstallState, fn func(*ssh.Client) error) error {
	conn, err := dialSSH(ctx, sshConfig{Host: state.Input.SSHHost, Port: state.Input.SSHPort, User: h2AgentUser, PrivateKey: state.PrivateKey})
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

func normalizeInstallSteps(steps []Step, attach bool) []Step {
	if len(steps) == 0 {
		return InitialSteps(attach)
	}
	out := make([]Step, len(steps))
	for i, step := range steps {
		if step.Type == "" {
			step.Type = inferStepType(step.Name, attach)
		}
		if step.Name == "" || step.Name == step.Type {
			step.Name = stepDisplayName(step.Type, attach)
		}
		out[i] = step
	}
	return out
}

func inferStepType(name string, attach bool) string {
	switch name {
	case "Подключение к VPS", "Connecting to VPS":
		return StepTypeConnect
	case "Проверка системы", "Checking system":
		return StepTypeCheckSystem
	case "Создание SSH-пользователя Hysteria agent", "Creating Hysteria agent SSH user":
		return StepTypeCreateUser
	case "Поиск существующей Hysteria2", "Установка Hysteria2", "Detecting existing Hysteria2", "Installing Hysteria2":
		return StepTypeInstallDetect
	case "Запись конфигурации Hysteria2", "Writing Hysteria2 config":
		return StepTypeWriteConfig
	case "Регистрация сервера", "Регистрация node", "Registering node":
		return StepTypeRegisterNode
	case "Установка LazGate Agent", "Installing LazGate Agent":
		return StepTypeInstallAgent
	case "Запуск сервиса Hysteria2", "Starting Hysteria2 service":
		return StepTypeStartService
	case "Проверка сервиса", "Verifying service":
		return StepTypeVerify
	case "Ожидание LazGate Agent", "Waiting for LazGate Agent":
		return StepTypeWaitAgent
	case "Готово", "Done":
		return StepTypeDone
	default:
		return name
	}
}

func findStep(steps []Step, stepType string) Step {
	for _, step := range steps {
		if step.Type == stepType {
			return step
		}
	}
	return Step{}
}
