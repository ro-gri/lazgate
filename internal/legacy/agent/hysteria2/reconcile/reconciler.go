package reconcile

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	masqueradefiles "laz/internal/agent/hysteria2/files"
	"laz/internal/protocol/hy2config"
	"laz/internal/protocol/hy2files"
)

type Options struct{ NodeID, ConfigPath, ServiceName, StatsURL, StatsSecret string }
type Reconciler struct {
	options Options
	mu      sync.Mutex
	run     func(context.Context, string, ...string) ([]byte, error)
}

func New(options ...Options) *Reconciler {
	r := &Reconciler{run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}}
	if len(options) > 0 {
		r.options = options[0]
	}
	return r
}

func (r *Reconciler) EditServerConfig(ctx context.Context, command *hysteriaproto.EditServerConfig) *commonproto.ServerConfigApplyResult {
	result := &commonproto.ServerConfigApplyResult{Status: "failed"}
	if command == nil {
		result.Status = "rejected"
		result.Error = "edit server config command is required"
		return result
	}
	result.NodeId = command.GetNodeId()
	result.DesiredRevision = command.GetRevision()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.apply(ctx, command); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Status = "applied"
	result.AppliedRevision = command.GetRevision()
	return result
}

func (r *Reconciler) apply(ctx context.Context, command *hysteriaproto.EditServerConfig) error {
	if command.GetNodeId() != r.options.NodeID || r.options.ConfigPath == "" || command.GetRevision() == 0 || command.GetServerConfig() == nil {
		return errors.New("invalid Hysteria configuration command")
	}
	c := hy2config.Defaults()
	d := json.NewDecoder(strings.NewReader(string(command.GetServerConfig().GetSettingsJson())))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return err
	}
	if err := hy2config.Validate(c); err != nil {
		return err
	}
	domain := command.GetServerConfig().GetEndpointHost()
	if !hy2config.Domain(domain) {
		return errors.New("invalid endpoint domain")
	}
	path := r.options.ConfigPath
	original, err := r.run(ctx, "sudo", "cat", path)
	if err != nil {
		return errors.New("cannot read Hysteria configuration")
	}
	backup := path + ".lazgate-settings-previous"
	_, backupErr := os.Stat(backup)
	if backupErr == nil {
		original, err = r.run(ctx, "sudo", "cat", backup)
		if err != nil {
			return errors.New("cannot read pending rollback configuration")
		}
	}
	var current map[string]any
	if err = yaml.Unmarshal(original, &current); err != nil {
		return err
	}
	if current == nil {
		return errors.New("empty Hysteria configuration")
	}
	previousDir := ""
	if masq, ok := current["masquerade"].(map[string]any); ok {
		if file, ok := masq["file"].(map[string]any); ok {
			previousDir, _ = file["dir"].(string)
		}
	}
	if _, ok := current["acme"]; !ok {
		tls, _ := current["tls"].(map[string]any)
		certPath, _ := tls["cert"].(string)
		raw, err := r.run(ctx, "sudo", "cat", certPath)
		if err != nil {
			return errors.New("cannot read TLS certificate")
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return errors.New("invalid TLS certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		if err = cert.VerifyHostname(domain); err != nil {
			return fmt.Errorf("endpoint is not covered by the local TLS certificate: %w", err)
		}
	}
	manager := masqueradefiles.Manager{Root: hy2files.ManagedDirectory, Run: r.run}
	activeDir := ""
	if id := command.GetServerConfig().GetMasqueradeFilesetId(); id != "" {
		if c.Masquerade.Type != "file" {
			return errors.New("file set supplied for non-file masquerade")
		}
		activeDir, err = manager.Stage(ctx, id, command.GetServerConfig().GetMasqueradeArchive())
		if err != nil {
			return err
		}
		c.Masquerade.Dir = activeDir
	} else if len(command.GetServerConfig().GetMasqueradeArchive()) > 0 {
		return errors.New("file set ID is required")
	}
	current, err = c.Runtime(current, domain)
	if err != nil {
		return err
	}
	// JSON normalization retains Hysteria's camelCase keys when YAML is emitted.
	encoded, err := json.Marshal(current)
	if err != nil {
		return err
	}
	var normalized map[string]any
	if err = json.Unmarshal(encoded, &normalized); err != nil {
		return err
	}
	raw, err := yaml.Marshal(normalized)
	if err != nil {
		return err
	}
	if string(raw) == string(original) && backupErr != nil {
		return r.healthy(ctx)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine config ownership")
	}
	write := func(ctx context.Context, content []byte) error {
		tmp, e := os.CreateTemp("", "laz-h2-settings-*")
		if e != nil {
			return e
		}
		name := tmp.Name()
		defer os.Remove(name)
		if _, e = tmp.Write(content); e != nil {
			tmp.Close()
			return e
		}
		if e = tmp.Close(); e != nil {
			return e
		}
		staged := filepath.Join(filepath.Dir(path), ".lazgate-config-pending")
		if _, e = r.run(ctx, "sudo", "install", "-m", fmt.Sprintf("%o", info.Mode().Perm()), "-o", fmt.Sprint(stat.Uid), "-g", fmt.Sprint(stat.Gid), name, staged); e != nil {
			return errors.New("cannot stage Hysteria configuration")
		}
		if _, e = r.run(ctx, "sudo", "mv", "-f", staged, path); e != nil {
			return errors.New("cannot replace Hysteria configuration")
		}
		return nil
	}
	if backupErr != nil {
		if _, err = r.run(ctx, "sudo", "cp", "-p", path, backup); err != nil {
			return errors.New("cannot preserve rollback configuration")
		}
	}
	if err = write(ctx, raw); err != nil {
		return err
	}
	restart := func(ctx context.Context) error {
		if _, err := r.run(ctx, "sudo", "systemctl", "restart", r.options.ServiceName); err != nil {
			return errors.New("Hysteria restart failed")
		}
		return r.healthy(ctx)
	}
	if err = restart(ctx); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := write(rollbackCtx, original); e != nil {
			return fmt.Errorf("apply failed and configuration rollback failed: %w", e)
		}
		if e := restart(rollbackCtx); e != nil {
			return fmt.Errorf("apply failed and service rollback failed: %w", e)
		}
		_, _ = r.run(rollbackCtx, "sudo", "rm", "-f", backup)
		return errors.New("Hysteria did not become healthy; previous settings restored")
	}
	if _, err = r.run(ctx, "sudo", "rm", "-f", backup); err != nil {
		return errors.New("cannot finalize configuration apply")
	}
	if err := manager.Cleanup(ctx, activeDir, previousDir); err != nil {
		slog.Warn("masquerade file cleanup failed", "error", err)
	}
	return nil
}
func (r *Reconciler) healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	stable := 0
	for ctx.Err() == nil {
		_, err := r.run(ctx, "sudo", "systemctl", "is-active", "--quiet", r.options.ServiceName)
		if err == nil && r.options.StatsURL != "" {
			req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(r.options.StatsURL, "/")+"/online", nil)
			if e != nil {
				return e
			}
			req.Header.Set("Authorization", r.options.StatsSecret)
			res, e := client.Do(req)
			err = e
			if res != nil {
				res.Body.Close()
				if res.StatusCode != 200 {
					err = errors.New("statistics API unavailable")
				}
			}
		}
		if err == nil {
			stable++
			if stable >= 3 {
				return nil
			}
		} else {
			stable = 0
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	return errors.New("Hysteria health check timed out")
}
