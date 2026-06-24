package reconcile

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/legacy/agent/amneziawg/tools"
	"laz/internal/protocol/awgconfig"
)

const (
	SentinelAllowedIP      = "0.0.0.0/32"
	maxAmneziaWGParamValue = 65535
)

var base64ishSecret = regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}`)

type Tools interface {
	ShowDump(ctx context.Context) (tools.InterfaceDump, error)
	SetPeer(ctx context.Context, publicKey string, opts tools.PeerOptions) error
	SetPeerAllowedIPs(ctx context.Context, publicKey string, allowedIPs []string) error
	RemovePeer(ctx context.Context, publicKey string) error
	SetInterfaceConfig(ctx context.Context, config tools.InterfaceConfig) error
}

type Reconciler struct {
	tools Tools
}

func New(t Tools) *Reconciler {
	return &Reconciler{tools: t}
}

func (r *Reconciler) EditServerConfig(ctx context.Context, command *amneziaproto.EditServerConfig) *commonproto.ServerConfigApplyResult {
	res := serverConfigResultFor(command)
	if r == nil || r.tools == nil {
		res.Status = "failed"
		res.Error = "AmneziaWG tools are not configured"
		return res
	}
	if command == nil {
		res.Status = "rejected"
		res.Error = "edit server config command is required"
		return res
	}
	content := command.GetServerConfig()
	config, err := interfaceConfigFromProto(content)
	if err != nil {
		res.Status = "rejected"
		res.Error = err.Error()
		return res
	}
	if err := r.tools.SetInterfaceConfig(ctx, config); err != nil {
		res.Status = "failed"
		res.Error = sanitize(err)
		return res
	}
	res.Status = "applied"
	res.AppliedRevision = command.GetRevision()
	return res
}

func (r *Reconciler) UpsertPeer(ctx context.Context, credential *amneziaproto.Credential) error {
	if r == nil || r.tools == nil {
		return fmt.Errorf("AmneziaWG tools are not configured")
	}
	publicKey := strings.TrimSpace(credential.GetPublicKey())
	if publicKey == "" {
		return fmt.Errorf("peer public key is required")
	}
	allowed := SentinelAllowedIP
	if isActive(credential.GetDesiredStatus()) {
		if err := validateAssignedIPv4(credential.GetAssignedIpv4()); err != nil {
			return err
		}
		allowed = strings.TrimSpace(credential.GetAssignedIpv4()) + "/32"
		if err := validateAllowedIPv4List(allowed); err != nil {
			return err
		}
	}
	if err := r.tools.SetPeer(ctx, publicKey, tools.PeerOptions{AllowedIPs: []string{allowed}, PresharedKey: strings.TrimSpace(credential.GetPresharedKey()), PersistentKeepalive: int(credential.GetPersistentKeepalive())}); err != nil {
		return fmt.Errorf("set peer: %s", sanitize(err))
	}
	return nil
}

func (r *Reconciler) RemovePeer(ctx context.Context, publicKey string) error {
	if r == nil || r.tools == nil {
		return fmt.Errorf("AmneziaWG tools are not configured")
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return fmt.Errorf("peer public key is required")
	}
	if err := r.tools.RemovePeer(ctx, publicKey); err != nil {
		return fmt.Errorf("remove peer: %s", sanitize(err))
	}
	return nil
}

func (r *Reconciler) PruneRuntimePeers(ctx context.Context, desired map[string]bool) error {
	if r == nil || r.tools == nil {
		return fmt.Errorf("AmneziaWG tools are not configured")
	}
	dump, err := r.tools.ShowDump(ctx)
	if err != nil {
		return err
	}
	for _, publicKey := range sortedKeys(mapRuntimePeers(dump)) {
		if !desired[publicKey] {
			if err := r.tools.RemovePeer(ctx, publicKey); err != nil {
				return err
			}
		}
	}
	return nil
}

func interfaceConfigFromProto(content *amneziaproto.ServerConfig) (tools.InterfaceConfig, error) {
	if content == nil {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config is required")
	}
	config := tools.InterfaceConfig{
		HeaderProtectionKey:    content.GetHeaderProtectionKey(),
		ContentPaddingAddition: content.GetContentPaddingAddition(),
		PrivateKey:             strings.TrimSpace(content.GetServerPrivateKey()),
		JC:                     int(content.GetJc()), JMin: int(content.GetJmin()), JMax: int(content.GetJmax()),
		S1: int(content.GetS1()), S2: int(content.GetS2()), S3: int(content.GetS3()), S4: int(content.GetS4()),
		H1: content.GetH1(), H2: content.GetH2(), H3: content.GetH3(), H4: content.GetH4(),
		I1: content.GetI1(), I2: content.GetI2(), I3: content.GetI3(), I4: content.GetI4(), I5: content.GetI5(),
		Timings: content.GetTimings(), RandomTrailers: content.GetRandomTrailers(), DisableCookies: content.GetDisableCookies(),
	}
	if config.PrivateKey == "" || strings.ContainsAny(config.PrivateKey, "\r\n\x00") {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config.server_private_key is invalid")
	}
	serverIP := net.ParseIP(strings.TrimSpace(content.GetServerIpv4())).To4()
	_, network, err := net.ParseCIDR(strings.TrimSpace(content.GetClientIpv4Cidr()))
	if serverIP == nil || err != nil || network.IP.To4() == nil || !network.Contains(serverIP) {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config server IPv4 and client IPv4 CIDR are invalid")
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones < 1 {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config client IPv4 CIDR is invalid")
	}
	config.Address = serverIP.String() + "/" + fmt.Sprintf("%d", ones)
	config.ListenPort = int(content.GetListenPort())
	if config.ListenPort < 1 || config.ListenPort > 65535 {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config.listen_port must be between 1 and 65535")
	}
	values := map[string]int{
		"jc": config.JC, "jmin": config.JMin, "jmax": config.JMax,
		"s1": config.S1, "s2": config.S2, "s3": config.S3, "s4": config.S4,
	}
	for name, value := range values {
		if value < 0 {
			return tools.InterfaceConfig{}, fmt.Errorf("server_config.%s must be greater than zero", name)
		}
		if value > maxAmneziaWGParamValue {
			return tools.InterfaceConfig{}, fmt.Errorf("server_config.%s must be <= %d", name, maxAmneziaWGParamValue)
		}
	}
	if err := awgconfig.Validate([4]string{config.H1, config.H2, config.H3, config.H4}, [5]string{config.I1, config.I2, config.I3, config.I4, config.I5}, config.ContentPaddingAddition, config.Timings); err != nil {
		return tools.InterfaceConfig{}, err
	}
	if config.JMin > config.JMax {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config.jmin must be <= server_config.jmax")
	}
	if config.HeaderProtectionKey != "" && (config.S1 < 12 || config.S2 < 12 || config.S3 < 12 || config.S4 < 12) {
		return tools.InterfaceConfig{}, fmt.Errorf("server_config 3.1 S1-S4 must be >= 12")
	}
	return config, nil
}

func validateAssignedIPv4(value string) error {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("value must be an IPv4 address")
	}
	return nil
}

func validateAllowedIPv4List(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("allowed IP entry must not be empty")
		}
		ip, _, err := net.ParseCIDR(part)
		if err != nil {
			return fmt.Errorf("allowed IP must be IPv4 CIDR")
		}
		if ip.To4() == nil {
			return fmt.Errorf("allowed IP must be IPv4 CIDR")
		}
	}
	return nil
}

func isActive(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "active")
}

func mapRuntimePeers(dump tools.InterfaceDump) map[string]tools.PeerDump {
	out := make(map[string]tools.PeerDump, len(dump.Peers))
	for _, p := range dump.Peers {
		out[p.PublicKey] = p
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func serverConfigResultFor(command *amneziaproto.EditServerConfig) *commonproto.ServerConfigApplyResult {
	res := &commonproto.ServerConfigApplyResult{Status: "applied"}
	if command != nil {
		res.NodeId = command.GetNodeId()
		res.DesiredRevision = command.GetRevision()
	}
	return res
}

func sanitize(v any) string {
	msg := fmt.Sprint(v)
	msg = base64ishSecret.ReplaceAllString(msg, "[redacted]")
	return strings.ReplaceAll(msg, "\n", " ")
}
