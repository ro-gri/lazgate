package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"laz/internal/server/integrations/nativehy2"
	"laz/internal/server/model"
	storeutil "laz/internal/server/storage"

	"golang.org/x/crypto/ssh"
)

const (
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
	Mode            string         `json:"mode"`
	Input           Hysteria2Input `json:"input"`
	GeneratedDomain bool           `json:"generated_domain"`
	NodeID          string         `json:"node_id"`
	PublicDomain    string         `json:"public_domain"`
	ServiceName     string         `json:"service_name"`
	ObfsPassword    string         `json:"obfs_password"`
	StatsSecret     string         `json:"stats_secret"`
	PrivateKey      string         `json:"private_key"`
	PublicKey       string         `json:"public_key"`
	CAPEM           string         `json:"ca_pem"`
	NodeCertPEM     string         `json:"node_cert_pem"`
	NodeKeyPEM      string         `json:"node_key_pem"`
	Node            model.Node     `json:"node"`
	Steps           []Step         `json:"steps"`
	Logs            []string       `json:"logs"`
}

type StepResult struct {
	State InstallState
	Step  Step
	Done  bool
}

func InitialInstallState(mode string, input Hysteria2Input) (InstallState, error) {
	input = normalize(input)
	r := &runner{installer: nil, input: input, attach: mode == "attach"}
	if err := r.validate(); err != nil {
		return InstallState{}, err
	}
	generatedDomain := false
	if input.PublicDomain == "" {
		input.PublicDomain = generatedDomainForHost(input.SSHHost)
		generatedDomain = true
	}
	obfsPassword, err := randomSecret()
	if err != nil {
		return InstallState{}, err
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
	certs, err := generateMTLSFiles(nodeID)
	if err != nil {
		return InstallState{}, err
	}
	return InstallState{
		Mode:            mode,
		Input:           input,
		GeneratedDomain: generatedDomain,
		NodeID:          nodeID,
		PublicDomain:    input.PublicDomain,
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
	r := &runner{installer: i, input: state.Input, attach: state.Mode == "attach", steps: state.Steps, logs: state.Logs}
	r.secrets = []string{state.Input.BootstrapPassword, state.ObfsPassword, state.StatsSecret, state.PrivateKey, state.NodeKeyPEM}
	var stepName string
	var err error
	switch stepType {
	case StepTypeConnect:
		stepName = "Connecting to VPS"
		r.start(stepName)
		err = withBootstrap(ctx, state, func(conn *ssh.Client) error { return nil })
		if err == nil {
			r.ok(stepName, "Connected to target VPS.")
		}
	case StepTypeCheckSystem:
		stepName = "Checking system"
		err = withBootstrap(ctx, state, func(conn *ssh.Client) error { return r.preflight(ctx, conn) })
	case StepTypeCreateUser:
		stepName = "Creating Hysteria agent SSH user"
		err = withBootstrap(ctx, state, func(conn *ssh.Client) error { return r.createUser(ctx, conn, state.PublicKey) })
		if err == nil {
			err = withLazgate(ctx, state, func(conn *ssh.Client) error { return nil })
		}
		if err == nil {
			r.ok(stepName, "Dedicated Hysteria agent SSH user verified.")
		}
	case StepTypeInstallDetect:
		if state.Mode == "attach" {
			stepName = "Detecting existing Hysteria2"
		} else {
			stepName = "Installing Hysteria2"
		}
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.installOrDetectHysteria(ctx, conn) })
	case StepTypeWriteConfig:
		stepName = "Writing Hysteria2 config"
		err = withLazgate(ctx, state, func(conn *ssh.Client) error {
			serviceName, err := r.writeConfig(ctx, conn, state.ObfsPassword, state.StatsSecret)
			state.ServiceName = serviceName
			return err
		})
	case StepTypeInstallAgent:
		stepName = "Installing LazGate Agent"
		err = withLazgate(ctx, state, func(conn *ssh.Client) error {
			return r.installAgent(ctx, conn, state.NodeID, state.ServiceName, state.StatsSecret, mtlsFiles{CAPEM: state.CAPEM, NodeCertPEM: state.NodeCertPEM, NodeKeyPEM: state.NodeKeyPEM})
		})
	case StepTypeStartService:
		stepName = "Starting Hysteria2 service"
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.startService(ctx, conn, state.ServiceName) })
	case StepTypeVerify:
		stepName = "Verifying service"
		err = withLazgate(ctx, state, func(conn *ssh.Client) error { return r.verify(ctx, conn, state.ServiceName) })
	case StepTypeRegisterNode:
		stepName = "Registering node"
		r.start(stepName)
		state.Node, err = i.registerNode(state)
		if err == nil {
			r.ok(stepName, "Node registered in LazGate.")
		}
	case StepTypeWaitAgent:
		stepName = "Waiting for LazGate Agent"
		err = r.waitAgent(ctx, state.NodeID)
	case StepTypeDone:
		stepName = "Done"
		r.start(stepName)
		r.ok(stepName, "Hysteria2 VPS node installed and registered.")
	default:
		return StepResult{}, errors.New("unknown hysteria install step")
	}
	if err != nil && findStep(r.steps, stepName).Status != StepFailed {
		r.fail(stepName, err)
	}
	state.Steps = r.steps
	state.Logs = r.logs
	if err != nil {
		return StepResult{State: state, Step: findStep(r.steps, stepName)}, err
	}
	return StepResult{State: state, Step: findStep(r.steps, stepName), Done: stepType == StepTypeDone}, nil
}

func (i *Installer) registerNode(state InstallState) (model.Node, error) {
	meta := nativehy2.Metadata{
		PublicDomain: state.PublicDomain,
		ListenPort:   state.Input.HysteriaPort,
		ServiceName:  state.ServiceName,
		ObfsEnabled:  state.Input.ObfsEnabled,
		ObfsType:     state.Input.ObfsType,
		ObfsPassword: state.ObfsPassword,
		AgentEnabled: true,
		StatsURL:     "http://" + state.Input.TrafficStatsListen,
		NodeCertPEM:  state.NodeCertPEM,
	}
	rawMeta, _ := json.Marshal(meta)
	return i.store.CreateNode(model.Node{
		ID:         state.NodeID,
		Name:       state.Input.NodeName,
		Type:       model.NodeTypeNativeHy2,
		BaseURL:    "hy2://" + state.PublicDomain + ":" + strconv.Itoa(state.Input.HysteriaPort),
		APIKey:     string(rawMeta),
		Region:     "",
		SSHHost:    state.Input.SSHHost,
		SSHPort:    state.Input.SSHPort,
		SSHUser:    h2AgentUser,
		SSHKeyPath: state.PrivateKey,
		UseIPv6:    false,
	})
}

func withBootstrap(ctx context.Context, state InstallState, fn func(*ssh.Client) error) error {
	conn, err := dialSSH(ctx, sshConfig{Host: state.Input.SSHHost, Port: state.Input.SSHPort, User: state.Input.BootstrapUser, Password: state.Input.BootstrapPassword})
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

func findStep(steps []Step, name string) Step {
	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}
	return Step{Name: name, Status: StepPending}
}
