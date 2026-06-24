package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

var snapshotOptions = ini.LoadOptions{
	AllowNonUniqueSections: true, AllowShadows: true, AllowDuplicateShadowValues: true,
	IgnoreInlineComment: true, IgnoreContinuation: true, PreserveSurroundedQuote: true,
	KeyValueDelimiters: "=",
}

func loadSnapshot(path string) (*ini.File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read native AWG snapshot: %w", err)
	}
	return parseSnapshot(raw)
}

func parseSnapshot(raw []byte) (*ini.File, error) {
	cfg, err := ini.LoadSources(snapshotOptions, raw)
	if err != nil {
		return nil, errors.New("invalid native AWG snapshot INI")
	}
	interfaces, err := cfg.SectionsByName("Interface")
	if err != nil || len(interfaces) != 1 {
		return nil, errors.New("native AWG snapshot must contain one Interface section")
	}
	interfaceKeys := strings.Fields("PrivateKey ListenPort FwMark Jc Jmin Jmax S1 S2 S3 S4 H1 H2 H3 H4 I1 I2 I3 I4 I5 HeaderProtectionKey ContentPaddingAddition RekeyAfterTime RekeyTimeout RejectAfterTime KeepaliveTimeout MaxHandshakeAttempts RandomTrailers DisableCookies")
	peerKeys := strings.Fields("PublicKey PresharedKey AllowedIPs Endpoint PersistentKeepalive AdvancedSecurity")
	for _, section := range cfg.Sections() {
		var allowed []string
		switch section.Name() {
		case ini.DefaultSection:
			if len(section.Keys()) != 0 {
				return nil, errors.New("native AWG snapshot has keys outside a section")
			}
			continue
		case "Interface":
			allowed = interfaceKeys
		case "Peer":
			allowed = peerKeys
		default:
			return nil, errors.New("native AWG snapshot has an unsupported section")
		}
		for _, key := range section.Keys() {
			found := false
			for _, name := range allowed {
				if key.Name() == name {
					found = true
					break
				}
			}
			if !found {
				return nil, errors.New("native AWG snapshot contains a non-native setting")
			}
			for _, value := range key.ValueWithShadows() {
				if err := singleLine(value); err != nil {
					return nil, err
				}
			}
		}
	}
	return cfg, nil
}

// Keep native values literal (not INI-quoted), including AWG I1-I5 expressions.
func encodeSnapshot(cfg *ini.File) []byte {
	var out bytes.Buffer
	for _, section := range cfg.Sections() {
		if section.Name() == ini.DefaultSection {
			continue
		}
		fmt.Fprintf(&out, "[%s]\n", section.Name())
		for _, key := range section.Keys() {
			values := key.ValueWithShadows()
			if len(values) == 0 {
				values = []string{""}
			}
			for _, value := range values {
				fmt.Fprintf(&out, "%s = %s\n", key.Name(), value)
			}
		}
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func (f *Facade) updateSnapshot(update func(*ini.File) error) error {
	if f.configPath == "" {
		return nil
	}
	cfg, err := loadSnapshot(f.configPath)
	if err != nil {
		return err
	}
	if update != nil {
		if err := update(cfg); err != nil {
			return err
		}
	}
	return atomicSnapshot(f.configPath, encodeSnapshot(cfg))
}

func atomicSnapshot(path string, raw []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".awg-snapshot-*")
	if err != nil {
		return fmt.Errorf("create native AWG snapshot: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if _, err = file.Write(raw); err != nil {
		return fmt.Errorf("write native AWG snapshot: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync native AWG snapshot: %w", err)
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("replace native AWG snapshot: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func singleLine(values ...string) error {
	for _, value := range values {
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("native AWG config values must be single-line")
		}
	}
	return nil
}

func (f *Facade) persistPeer(publicKey string, opts PeerOptions, remove bool) error {
	if err := singleLine(publicKey, opts.PresharedKey, opts.Endpoint); err != nil {
		return err
	}
	return f.updateSnapshot(func(cfg *ini.File) error {
		peers, _ := cfg.SectionsByName("Peer")
		var target *ini.Section
		for index := len(peers) - 1; index >= 0; index-- {
			if peers[index].Key("PublicKey").Value() != publicKey {
				continue
			}
			if remove {
				if err := cfg.DeleteSectionWithIndex("Peer", index); err != nil {
					return err
				}
				continue
			}
			if target != nil {
				return errors.New("duplicate public key in native AWG snapshot")
			}
			target = peers[index]
		}
		if remove {
			return nil
		}
		if target == nil {
			target, _ = cfg.NewSection("Peer")
			target.Key("PublicKey").SetValue(publicKey)
		}
		if opts.AllowedIPs != nil {
			target.DeleteKey("AllowedIPs")
			target.Key("AllowedIPs").SetValue(strings.Join(opts.AllowedIPs, ","))
		}
		if opts.PresharedKey != "" {
			target.DeleteKey("PresharedKey")
			target.Key("PresharedKey").SetValue(strings.TrimSpace(opts.PresharedKey))
		}
		if opts.Endpoint != "" {
			target.DeleteKey("Endpoint")
			target.Key("Endpoint").SetValue(opts.Endpoint)
		}
		if opts.PersistentKeepalive > 0 {
			target.DeleteKey("PersistentKeepalive")
			target.Key("PersistentKeepalive").SetValue(fmt.Sprint(opts.PersistentKeepalive))
		}
		return nil
	})
}

func (f *Facade) snapshotInterface(ctx context.Context) error {
	if f.configPath == "" {
		return nil
	}
	result, err := f.runtime.RunSecretOutput(ctx, f.execArgv("awg", "showconf", f.iface))
	if err != nil {
		return err
	}
	runtimeConfig, err := parseSnapshot([]byte(result.Stdout))
	if err != nil {
		return errors.New("invalid native AWG showconf response")
	}
	// Runtime may be missing pending desired peers. Refresh only the interface.
	return f.updateSnapshot(func(cfg *ini.File) error {
		section := cfg.Section("Interface")
		for _, key := range section.Keys() {
			section.DeleteKey(key.Name())
		}
		for _, key := range runtimeConfig.Section("Interface").Keys() {
			for i, value := range key.ValueWithShadows() {
				if i == 0 {
					section.Key(key.Name()).SetValue(value)
				} else if err := section.Key(key.Name()).AddShadow(value); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// PrunePeers also removes pending desired peers that never reached runtime.
func (f *Facade) PrunePeers(_ context.Context, keep map[string]bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updateSnapshot(func(cfg *ini.File) error {
		peers, _ := cfg.SectionsByName("Peer")
		for index := len(peers) - 1; index >= 0; index-- {
			if !keep[peers[index].Key("PublicKey").Value()] {
				if err := cfg.DeleteSectionWithIndex("Peer", index); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Restore applies the durable desired state without contacting the server.
func (f *Facade) Restore(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restore(ctx)
}

func (f *Facade) restore(ctx context.Context) error {
	if f.configPath == "" {
		return nil
	}
	cfg, err := loadSnapshot(f.configPath)
	if errors.Is(err, os.ErrNotExist) {
		// Seed exactly once from the local interface, never from the control plane.
		// Existing desired snapshots always win over potentially stale runtime.
		result, runErr := f.runtime.RunSecretOutput(ctx, f.execArgv("awg", "showconf", f.iface))
		if runErr != nil {
			return errors.New("seed native AWG snapshot: local showconf failed")
		}
		cfg, err = parseSnapshot([]byte(result.Stdout))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(f.configPath), 0700); err != nil {
			return err
		}
		if err := atomicSnapshot(f.configPath, encodeSnapshot(cfg)); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	raw := encodeSnapshot(cfg)
	secrets := []string{string(raw)}
	for _, section := range cfg.Sections() {
		for _, name := range []string{"PrivateKey", "PresharedKey", "HeaderProtectionKey"} {
			if key, err := section.GetKey(name); err == nil {
				secrets = append(secrets, key.Value())
			}
		}
	}
	_, err = f.runtime.Run(ctx, f.execArgv("awg", "syncconf", f.iface, "/dev/stdin"), raw, secrets...)
	if err != nil {
		return err
	}
	return f.saveHostConfig(ctx)
}
