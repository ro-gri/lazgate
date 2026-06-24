package tools

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"

	"laz/internal/legacy/agent/amneziawg/container"
	"laz/internal/protocol/awgconfig"
)

const (
	DefaultInterface = "awg0"
	DefaultLogLines  = 80
	pskTmpfsPrefix   = "/dev/shm/lazgate-awg-psk-"
	keyTmpfsPrefix   = "/dev/shm/lazgate-awg-key-"

	hostAWGScript = `
awg_binary=$1
use_sudo=$2
shift 2
awg() {
	if [ "$use_sudo" = true ]; then sudo -n "$awg_binary" "$@"; else command "$awg_binary" "$@"; fi
}
`
	pskStdinScript = hostAWGScript + `
if [ ! -d /dev/shm ] || [ ! -w /dev/shm ]; then
	echo "PSK tmpfs unavailable: /dev/shm must be a writable tmpfs mount on the host" >&2
	exit 1
fi
if ! awk '$5 == "/" || $5 == "/dev" || $5 == "/dev/shm" { split($0, fields, " - "); split(fields[2], fs); visible_fstype = fs[1] } END { exit visible_fstype != "tmpfs" }' /proc/self/mountinfo; then
	echo "PSK tmpfs unavailable: /dev/shm visible mount is not tmpfs; check /proc/self/mountinfo" >&2
	exit 1
fi
dir=$3
case "$dir" in
	/dev/shm/lazgate-awg-psk-[0-9a-f]*) ;;
	*)
		echo "PSK tmpfs unavailable: invalid temporary directory path" >&2
		exit 1
		;;
esac
if ! mkdir -m 700 "$dir"; then
	echo "PSK tmpfs unavailable: cannot create a temporary directory in /dev/shm" >&2
	exit 1
fi
cleanup() {
	if [ -n "${dir:-}" ]; then
		rm -rf "$dir"
		dir=
	fi
}
on_signal() {
	cleanup
	trap - EXIT
	exit 1
}
trap cleanup EXIT
trap on_signal HUP INT TERM
iface=$1
public_key=$2
shift 3
psk_path="$dir/psk"
umask 077
cat > "$psk_path"
awg set "$iface" peer "$public_key" "$@" preshared-key "$psk_path"
`
	interfaceConfigStdinScript = hostAWGScript + `
if [ ! -d /dev/shm ] || [ ! -w /dev/shm ]; then
	echo "private key tmpfs unavailable: /dev/shm must be a writable tmpfs mount on the host" >&2
	exit 1
fi
if ! awk '$5 == "/" || $5 == "/dev" || $5 == "/dev/shm" { split($0, fields, " - "); split(fields[2], fs); visible_fstype = fs[1] } END { exit visible_fstype != "tmpfs" }' /proc/self/mountinfo; then
	echo "private key tmpfs unavailable: /dev/shm visible mount is not tmpfs" >&2
	exit 1
fi
iface=$1
dir=$2
shift 2
case "$dir" in
	/dev/shm/lazgate-awg-key-[0-9a-f]*) ;;
	*) echo "invalid private key temporary directory" >&2; exit 1 ;;
esac
mkdir -m 700 "$dir"
cleanup() { rm -rf "$dir"; }
on_signal() { cleanup; trap - EXIT; exit 1; }
trap cleanup EXIT
trap on_signal HUP INT TERM
key_path="$dir/private-key"
umask 077
IFS= read -r private_key
IFS= read -r header_key
printf '%s\n' "$private_key" > "$key_path"
header_path="$dir/header-key"
printf '%s\n' "$header_key" > "$header_path"
awg set "$iface" private-key "$key_path" header-protection-key "$header_path" "$@"
`
)

type Facade struct {
	runtime        *container.Runtime
	iface          string
	logLines       int
	service        string
	awgBinary      string
	useSudo        bool
	configPath     string
	hostConfigPath string
	mu             sync.Mutex
}

type Option func(*Facade)

func WithConfigPath(path string) Option     { return func(f *Facade) { f.configPath = path } }
func WithHostConfigPath(path string) Option { return func(f *Facade) { f.hostConfigPath = path } }

func WithServiceName(name string) Option {
	return func(f *Facade) { f.service = strings.TrimSpace(name) }
}
func WithAWGBinaryPath(path string) Option {
	return func(f *Facade) {
		if strings.TrimSpace(path) != "" {
			f.awgBinary = strings.TrimSpace(path)
		}
	}
}
func WithSudo(enabled bool) Option { return func(f *Facade) { f.useSudo = enabled } }

func WithRuntime(rt *container.Runtime) Option {
	return func(f *Facade) {
		if rt != nil {
			f.runtime = rt
		}
	}
}

func WithInterfaceName(name string) Option {
	return func(f *Facade) {
		if name = strings.TrimSpace(name); name != "" {
			f.iface = name
		}
	}
}

func WithLogLines(lines int) Option {
	return func(f *Facade) {
		if lines > 0 {
			f.logLines = lines
		}
	}
}

func New(options ...Option) *Facade {
	f := &Facade{
		runtime:   container.New(),
		iface:     DefaultInterface,
		logLines:  DefaultLogLines,
		awgBinary: "awg",
	}
	for _, option := range options {
		option(f)
	}
	return f
}

func (f *Facade) ShowDump(ctx context.Context) (InterfaceDump, error) {
	res, err := f.runtime.RunSecretOutput(ctx, f.execArgv("awg", "show", f.iface, "dump"))
	if err != nil {
		return InterfaceDump{}, err
	}
	return ParseShowDump(res.Stdout)
}

func (f *Facade) SetPeer(ctx context.Context, publicKey string, opts PeerOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, err := f.setPeerPlan(publicKey, opts)
	if err != nil {
		return err
	}
	if err := f.persistPeer(publicKey, opts, false); err != nil {
		return err
	}
	return f.runSetPeer(ctx, plan)
}

func (f *Facade) SetPeerAllowedIPs(ctx context.Context, publicKey string, allowedIPs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(publicKey) == "" {
		return errors.New("public key is required")
	}
	validatedAllowedIPs, err := validateAllowedIPv4CIDRs(allowedIPs)
	if err != nil {
		return fmt.Errorf("invalid allowed ips: %w", err)
	}
	argv := f.execArgv("awg", "set", f.iface, "peer", publicKey, "allowed-ips", strings.Join(validatedAllowedIPs, ","))
	if err := f.persistPeer(publicKey, PeerOptions{AllowedIPs: validatedAllowedIPs}, false); err != nil {
		return err
	}
	_, err = f.runtime.Run(ctx, argv, nil)
	if err != nil {
		return err
	}
	return f.saveHostConfig(ctx)
}

func (f *Facade) RemovePeer(ctx context.Context, publicKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(publicKey) == "" {
		return errors.New("public key is required")
	}
	argv := f.execArgv("awg", "set", f.iface, "peer", publicKey, "remove")
	if err := f.persistPeer(publicKey, PeerOptions{}, true); err != nil {
		return err
	}
	_, err := f.runtime.Run(ctx, argv, nil)
	if err != nil {
		return err
	}
	return f.saveHostConfig(ctx)
}

func (f *Facade) Restart(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Validate durable desired state before restarting the root-owned base service.
	if err := f.updateSnapshot(nil); err != nil {
		return err
	}
	_, err := f.runtime.Run(ctx, f.execArgv("systemctl", "restart", f.serviceUnit()), nil)
	if err != nil {
		return err
	}
	return f.restore(ctx)
}

func (f *Facade) Status(ctx context.Context) (string, error) {
	res, err := f.runtime.Run(ctx, f.execArgv("systemctl", "show", "--property=ActiveState", "--value", f.serviceUnit()), nil)
	if err != nil {
		return "", err
	}
	status := strings.ToLower(strings.TrimSpace(res.Stdout))
	if status == "" {
		status = "unknown"
	}
	return status, nil
}

func (f *Facade) Inspect(ctx context.Context) (string, error) {
	res, err := f.runtime.Run(ctx, f.execArgv("systemctl", "show", f.serviceUnit()), nil)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func (f *Facade) Logs(ctx context.Context) (string, error) {
	res, err := f.runtime.Run(ctx, f.execArgv("journalctl", "--unit", f.serviceUnit(), "--lines", fmt.Sprintf("%d", f.logLines), "--no-pager"), nil)
	if err != nil {
		return "", err
	}
	return res.Stdout + res.Stderr, nil
}

type PeerOptions struct {
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
	PresharedKey        string
}

type InterfaceConfig struct {
	Timings                map[string]string
	RandomTrailers         bool
	DisableCookies         bool
	PrivateKey             string
	HeaderProtectionKey    string
	ContentPaddingAddition string
	Address                string
	ListenPort             int
	JC                     int
	JMin                   int
	JMax                   int
	S1                     int
	S2                     int
	S3                     int
	S4                     int
	H1                     string
	H2                     string
	H3                     string
	H4                     string
	I1                     string
	I2                     string
	I3                     string
	I4                     string
	I5                     string
}

func (f *Facade) SetInterfaceConfig(ctx context.Context, config InterfaceConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if config.H1 == "" {
		config.H1 = "1"
	}
	if config.H2 == "" {
		config.H2 = "2"
	}
	if config.H3 == "" {
		config.H3 = "3"
	}
	if config.H4 == "" {
		config.H4 = "4"
	}
	if err := awgconfig.Validate([4]string{config.H1, config.H2, config.H3, config.H4}, [5]string{config.I1, config.I2, config.I3, config.I4, config.I5}, config.ContentPaddingAddition, config.Timings); err != nil {
		return err
	}
	if strings.TrimSpace(config.PrivateKey) == "" {
		return errors.New("interface private key is required")
	}
	headerKey, err := base64.StdEncoding.DecodeString(config.HeaderProtectionKey)
	if err != nil || len(headerKey) != 32 || strings.ContainsAny(config.PrivateKey, "\r\n\x00") {
		return errors.New("interface header protection key must be a base64-encoded 32-byte key and private key must be single-line")
	}
	if config.S1 < 12 || config.S2 < 12 || config.S3 < 12 || config.S4 < 12 {
		return errors.New("interface S1-S4 must be at least 12 with header protection")
	}
	if _, _, err := net.ParseCIDR(strings.TrimSpace(config.Address)); err != nil {
		return errors.New("interface address must be CIDR")
	}
	if config.ListenPort < 1 || config.ListenPort > 65535 {
		return errors.New("interface listen port must be between 1 and 65535")
	}
	dir, err := newPrivateKeyTmpfsDir()
	if err != nil {
		return err
	}
	args := []string{"listen-port", fmt.Sprintf("%d", config.ListenPort),
		"jc", fmt.Sprintf("%d", config.JC),
		"jmin", fmt.Sprintf("%d", config.JMin),
		"jmax", fmt.Sprintf("%d", config.JMax),
		"s1", fmt.Sprintf("%d", config.S1),
		"s2", fmt.Sprintf("%d", config.S2),
		"s3", fmt.Sprintf("%d", config.S3),
		"s4", fmt.Sprintf("%d", config.S4),
		"h1", config.H1,
		"h2", config.H2,
		"h3", config.H3,
		"h4", config.H4,
	}
	for index, value := range []string{config.I1, config.I2, config.I3, config.I4, config.I5} {
		args = append(args, fmt.Sprintf("i%d", index+1), value)
	}
	args = append(args, "content-padding-addition", defaultParameter(config.ContentPaddingAddition), "random-trailers", awgconfig.FormatBool(config.RandomTrailers), "disable-cookies", awgconfig.FormatBool(config.DisableCookies))
	for _, key := range awgconfig.Timings {
		args = append(args, timingFlag(key), defaultParameter(config.Timings[key]))
	}
	argv := f.execWithStdinArgv("sh", "-ceu", interfaceConfigStdinScript, "sh", f.awgBinary, fmt.Sprint(f.useSudo), f.iface, dir)
	argv = append(argv, args...)
	_, err = f.runtime.Run(ctx, argv, []byte(strings.TrimSpace(config.PrivateKey)+"\n"+config.HeaderProtectionKey+"\n"), config.PrivateKey, config.HeaderProtectionKey)
	if err != nil {
		return err
	}
	if err := f.applyHostNetwork(ctx, hostNetworkConfig{Address: config.Address, ListenPort: config.ListenPort}); err != nil {
		return err
	}
	if err := f.snapshotInterface(ctx); err != nil {
		return err
	}
	return f.saveHostConfig(ctx)
}

func defaultParameter(value string) string {
	if value == "" || value == "(off)" {
		return "0"
	}
	return value
}
func timingFlag(key string) string {
	switch key {
	case "RekeyAfterTime":
		return "rekey-after-time"
	case "RekeyTimeout":
		return "rekey-timeout"
	case "RejectAfterTime":
		return "reject-after-time"
	case "KeepaliveTimeout":
		return "keepalive-timeout"
	default:
		return "max-handshake-attempts"
	}
}

type setPeerPlan struct {
	argv    []string
	stdin   []byte
	secrets []string
}

func (f *Facade) runSetPeer(ctx context.Context, plan setPeerPlan) error {
	_, err := f.runtime.Run(ctx, plan.argv, plan.stdin, plan.secrets...)
	if err != nil {
		return err
	}
	return f.saveHostConfig(ctx)
}

func (f *Facade) setPeerPlan(publicKey string, opts PeerOptions) (setPeerPlan, error) {
	if strings.TrimSpace(publicKey) == "" {
		return setPeerPlan{}, errors.New("public key is required")
	}
	plan := setPeerPlan{argv: f.execArgv("awg", "set", f.iface, "peer", publicKey)}
	if len(opts.AllowedIPs) > 0 {
		allowedIPs, err := validateAllowedIPv4CIDRs(opts.AllowedIPs)
		if err != nil {
			return setPeerPlan{}, fmt.Errorf("invalid allowed ips: %w", err)
		}
		plan.argv = append(plan.argv, "allowed-ips", strings.Join(allowedIPs, ","))
	}
	if strings.TrimSpace(opts.Endpoint) != "" {
		plan.argv = append(plan.argv, "endpoint", opts.Endpoint)
	}
	if opts.PersistentKeepalive > 0 {
		plan.argv = append(plan.argv, "persistent-keepalive", fmt.Sprintf("%d", opts.PersistentKeepalive))
	}
	if strings.TrimSpace(opts.PresharedKey) != "" {
		dir, err := newPSKTmpfsDir()
		if err != nil {
			return setPeerPlan{}, err
		}
		peerArgs := plan.argv[len(f.execArgv("awg", "set", f.iface, "peer", publicKey)):]
		scriptArgs := []string{"sh", "-ceu", pskStdinScript, "sh", f.awgBinary, fmt.Sprint(f.useSudo), f.iface, publicKey, dir}
		scriptArgs = append(scriptArgs, peerArgs...)
		plan.argv = f.execWithStdinArgv(scriptArgs...)
		plan.stdin = []byte(strings.TrimSpace(opts.PresharedKey) + "\n")
		plan.secrets = append(plan.secrets, opts.PresharedKey)
	}
	return plan, nil
}

func newPSKTmpfsDir() (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generate PSK tmpfs directory ID: %w", err)
	}
	return pskTmpfsPrefix + hex.EncodeToString(id), nil
}

func newPrivateKeyTmpfsDir() (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generate private key tmpfs directory ID: %w", err)
	}
	return keyTmpfsPrefix + hex.EncodeToString(id), nil
}

func (f *Facade) execArgv(args ...string) []string {
	if len(args) > 0 && args[0] == "awg-quick" && filepath.IsAbs(f.awgBinary) {
		args[0] = filepath.Join(filepath.Dir(f.awgBinary), "awg-quick")
	}
	if len(args) > 0 && args[0] == "awg" {
		args[0] = f.awgBinary
	}
	if f.useSudo {
		return append([]string{"sudo", "-n"}, args...)
	}
	return append([]string(nil), args...)
}

func (f *Facade) execWithStdinArgv(args ...string) []string {
	return append([]string(nil), args...)
}

func (f *Facade) serviceUnit() string {
	name := f.service
	if name == "" {
		name = "awg-quick@" + f.iface
	}
	return strings.TrimSuffix(name, ".service") + ".service"
}
