package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"laz/internal/protocol/hy2files"
	"laz/internal/server/model"

	"golang.org/x/crypto/ssh"
)

const (
	StepTypeUninstallCleanup = "hysteria.uninstall.cleanup"
	StepTypeDetachCleanup    = "hysteria.detach.cleanup"
)

type NodeSoftDeleter interface {
	GetNode(id string) (model.Node, error)
	GetActiveNodeInstance(nodeID string) (model.NodeInstance, error)
	SoftDeleteNode(id string) (model.Node, error)
}

type UninstallState struct {
	NodeID      string             `json:"node_id"`
	Node        model.Node         `json:"node"`
	Instance    model.NodeInstance `json:"instance"`
	ServiceName string             `json:"service_name"`
}

func (i *Installer) PrepareHysteria2Uninstall(nodeID string) (UninstallState, error) {
	store, ok := i.store.(NodeSoftDeleter)
	if !ok {
		return UninstallState{}, errors.New("удаление сервера не настроено")
	}
	node, err := store.GetNode(strings.TrimSpace(nodeID))
	if err != nil {
		return UninstallState{}, err
	}
	if node.Type != model.NodeTypeNativeHy2 {
		return UninstallState{}, errors.New("операция доступна только для native Hysteria2 серверов")
	}
	instance, err := store.GetActiveNodeInstance(node.ID)
	if err != nil {
		return UninstallState{}, err
	}
	if strings.TrimSpace(instance.Host) == "" || strings.TrimSpace(instance.SSHKey) == "" {
		return UninstallState{}, errors.New("SSH-доступ к серверу не настроен")
	}
	serviceName := "hysteria-server"
	return UninstallState{NodeID: node.ID, Node: node, Instance: instance, ServiceName: serviceName}, nil
}

func (i *Installer) SoftDeletePreparedNode(state UninstallState) (model.Node, error) {
	store, ok := i.store.(NodeSoftDeleter)
	if !ok {
		return model.Node{}, errors.New("удаление сервера не настроено")
	}
	return store.SoftDeleteNode(state.NodeID)
}

func (i *Installer) PrepareHysteria2Detach(nodeID string) (UninstallState, error) {
	return i.PrepareHysteria2Uninstall(nodeID)
}

func (i *Installer) ExecuteHysteriaUninstallStep(ctx context.Context, stepType string, state UninstallState) error {
	switch stepType {
	case StepTypeUninstallCleanup:
		return withNodeSSH(ctx, state.Instance, func(conn *ssh.Client) error {
			return runHysteriaUninstallCleanup(ctx, conn, state.ServiceName)
		})
	case StepTypeDetachCleanup:
		return withNodeSSH(ctx, state.Instance, func(conn *ssh.Client) error {
			return runHysteriaDetachCleanup(ctx, conn, state.ServiceName)
		})
	default:
		return errors.New("неизвестный шаг удаления Hysteria2 VPS")
	}
}

func withNodeSSH(ctx context.Context, instance model.NodeInstance, fn func(*ssh.Client) error) error {
	conn, err := dialSSH(ctx, sshConfig{Host: instance.Host, Port: instance.SSHPort, User: instance.SSHUser, PrivateKey: instance.SSHKey})
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

func runHysteriaDetachCleanup(ctx context.Context, conn *ssh.Client, serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		serviceName = "hysteria-server"
	}
	if _, err := runSSH(ctx, conn, "sudo test -f "+shellQuote(hysteriaAttachBackupPath)); err != nil {
		if detachAlreadyCleaned(ctx, conn) {
			return nil
		}
		return errors.New("Отключение невозможно: исходная конфигурация Hysteria не найдена. Сервер не был подключён через LazGate или backup повреждён")
	}
	currentRaw, err := runSSH(ctx, conn, "sudo cat "+shellQuote(hysteriaConfigPath))
	if err != nil {
		return err
	}
	backupRaw, err := runSSH(ctx, conn, "sudo cat "+shellQuote(hysteriaAttachBackupPath))
	if err != nil {
		return err
	}
	restored, err := restoreAttachManagedHysteriaConfig(currentRaw, backupRaw)
	if err != nil {
		return err
	}
	restoredBase64 := base64.StdEncoding.EncodeToString([]byte(restored))
	restoreConfigScript := fmt.Sprintf(`
sudo systemctl stop "$HYSTERIA_SERVICE" || true
sudo systemctl stop hysteria-server || true
sudo systemctl stop hysteria || true
printf '%%s' %[2]s | base64 -d | sudo tee %[1]s >/dev/null
if id hysteria >/dev/null 2>&1; then
  sudo chown hysteria:hysteria %[1]s
  sudo chmod 600 %[1]s
else
  sudo chmod 644 %[1]s
fi
sudo systemctl daemon-reload
sudo systemctl restart "$HYSTERIA_SERVICE"
`, shellQuote(hysteriaConfigPath), shellQuote(restoredBase64))
	script := fmt.Sprintf(`set -eu
HYSTERIA_SERVICE=%[1]s
AGENT_SERVICE=lazgate-agent
%[2]s
sudo systemctl stop "$AGENT_SERVICE" || true
sudo systemctl disable "$AGENT_SERVICE" || true
sudo rm -f /etc/systemd/system/lazgate-agent.service /lib/systemd/system/lazgate-agent.service /usr/lib/systemd/system/lazgate-agent.service || true
sudo rm -f /usr/local/bin/lazgate-agent /usr/bin/lazgate-agent || true
sudo rm -rf /etc/lazgate-agent /var/lib/lazgate-agent /var/cache/lazgate-agent /var/log/lazgate-agent || true

cleanup_script=$(mktemp /tmp/lazgate-agent-detach.XXXXXX.sh) || exit 0
cat > "$cleanup_script" <<'CLEANUP'
#!/bin/sh
set +e
sleep 2
AGENT_USER=h2-laz-agent
rm -f /etc/sudoers.d/h2-laz-agent || true
pkill -u "$AGENT_USER" || true
userdel -r "$AGENT_USER" || userdel "$AGENT_USER" || true
rm -rf /home/${AGENT_USER} || true
groupdel "$AGENT_USER" || true
systemctl daemon-reload || true
systemctl reset-failed || true
rm -f "$0" || true
CLEANUP
sudo chown root:root "$cleanup_script" || true
sudo chmod 700 "$cleanup_script" || true
sudo sh -c "nohup sh '$cleanup_script' >/dev/null 2>&1 &" || true
sudo rm -f %[3]s
`, shellQuote(serviceName), restoreConfigScript, shellQuote(hysteriaAttachBackupPath))
	_, err = runSSH(ctx, conn, script)
	return err
}

func detachAlreadyCleaned(ctx context.Context, conn *ssh.Client) bool {
	_, err := runSSH(ctx, conn, `
set -eu
! sudo test -f /etc/hysteria/config.yaml.lazgate-backup
! sudo systemctl cat lazgate-agent >/dev/null 2>&1
! command -v lazgate-agent >/dev/null 2>&1
! sudo test -e /etc/lazgate-agent
! sudo test -e /var/lib/lazgate-agent
! sudo test -e /var/log/lazgate-agent
! id h2-laz-agent >/dev/null 2>&1
`)
	return err == nil
}

func runHysteriaUninstallCleanup(ctx context.Context, conn *ssh.Client, serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		serviceName = "hysteria-server"
	}
	script := fmt.Sprintf(`set +e
HYSTERIA_SERVICE=%[1]s
AGENT_SERVICE=lazgate-agent
AGENT_USER=%[2]s

sudo systemctl stop "$HYSTERIA_SERVICE" || true
sudo systemctl stop hysteria-server || true
sudo systemctl stop hysteria || true
sudo systemctl stop "$AGENT_SERVICE" || true

sudo systemctl disable "$HYSTERIA_SERVICE" || true
sudo systemctl disable hysteria-server || true
sudo systemctl disable hysteria || true
sudo systemctl disable "$AGENT_SERVICE" || true

sudo rm -f "/etc/systemd/system/${HYSTERIA_SERVICE}.service" || true
sudo rm -f "/lib/systemd/system/${HYSTERIA_SERVICE}.service" || true
sudo rm -f "/usr/lib/systemd/system/${HYSTERIA_SERVICE}.service" || true
sudo rm -f /etc/systemd/system/hysteria-server.service || true
sudo rm -f /etc/systemd/system/hysteria.service || true
sudo rm -f /etc/systemd/system/lazgate-agent.service || true
sudo rm -f /lib/systemd/system/hysteria-server.service || true
sudo rm -f /lib/systemd/system/hysteria.service || true
sudo rm -f /usr/lib/systemd/system/hysteria-server.service || true
sudo rm -f /usr/lib/systemd/system/hysteria.service || true

sudo rm -f /usr/local/bin/hysteria /usr/bin/hysteria || true
sudo rm -rf /etc/hysteria /var/lib/hysteria /var/cache/hysteria /var/log/hysteria %[3]s || true
sudo rm -f /etc/logrotate.d/hysteria || true

sudo rm -f /usr/local/bin/lazgate-agent /usr/bin/lazgate-agent || true
sudo rm -rf /etc/lazgate-agent /var/lib/lazgate-agent /var/cache/lazgate-agent /var/log/lazgate-agent || true

cleanup_script=$(mktemp /tmp/lazgate-agent-uninstall.XXXXXX.sh) || exit 0
cat > "$cleanup_script" <<'CLEANUP'
#!/bin/sh
set +e
sleep 2
AGENT_USER=h2-laz-agent
rm -f /etc/sudoers.d/h2-laz-agent || true
pkill -u "$AGENT_USER" || true
userdel -r "$AGENT_USER" || userdel "$AGENT_USER" || true
rm -rf /home/${AGENT_USER} || true
groupdel "$AGENT_USER" || true
systemctl daemon-reload || true
systemctl reset-failed || true
rm -f "$0" || true
CLEANUP
sudo chown root:root "$cleanup_script" || true
sudo chmod 700 "$cleanup_script" || true
sudo sh -c "nohup sh '$cleanup_script' >/dev/null 2>&1 &" || true

exit 0`, shellQuote(serviceName), h2AgentUser, shellQuote(hy2files.ManagedDirectory))
	_, err := runSSH(ctx, conn, script)
	return err
}
