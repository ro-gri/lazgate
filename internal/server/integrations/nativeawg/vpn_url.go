package nativeawg

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// RenderVPNURL exports a client-only AmneziaVPN profile. The envelope follows
// amnezia-client's third-party AWG import model and Qt qCompress framing.
func RenderVPNURL(name, conf string) (string, error) {
	file, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true, AllowNonUniqueSections: true}, []byte(conf))
	if err != nil {
		return "", errors.New("invalid AmneziaWG client configuration")
	}
	interfaces, err := file.SectionsByName("Interface")
	if err != nil || len(interfaces) != 1 {
		return "", errors.New("AmneziaWG client interface is missing")
	}
	iface := interfaces[0]
	peers, err := file.SectionsByName("Peer")
	if err != nil || len(peers) != 1 {
		return "", errors.New("AmneziaWG client configuration must contain one peer")
	}
	peer := peers[0]
	host, portText, err := net.SplitHostPort(peer.Key("Endpoint").String())
	if err != nil || host == "" {
		return "", errors.New("invalid AmneziaWG endpoint")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("invalid AmneziaWG endpoint port")
	}
	client := map[string]any{
		"config": conf, "hostName": host, "port": port,
		"client_priv_key":       iface.Key("PrivateKey").String(),
		"client_ip":             iface.Key("Address").String(),
		"server_pub_key":        peer.Key("PublicKey").String(),
		"psk_key":               peer.Key("PresharedKey").String(),
		"mtu":                   iface.Key("MTU").String(),
		"persistent_keep_alive": peer.Key("PersistentKeepalive").String(),
		"allowed_ips":           peer.Key("AllowedIPs").Strings(","),
	}
	for _, key := range []string{"client_priv_key", "client_ip", "server_pub_key"} {
		if client[key] == "" {
			return "", errors.New("AmneziaWG client configuration is incomplete")
		}
	}
	awg := map[string]any{"isThirdPartyConfig": true, "port": portText, "transport_proto": "udp"}
	for _, key := range []string{
		"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4",
		"I1", "I2", "I3", "I4", "I5", "HeaderProtectionKey", "ContentPaddingAddition",
		"RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout",
		"MaxHandshakeAttempts", "RandomTrailers", "DisableCookies",
	} {
		if value := iface.Key(key).String(); value != "" {
			client[key] = value
			awg[key] = value
		}
	}
	version := "2"
	if iface.Key("HeaderProtectionKey").String() != "" {
		version = "3.1"
	}
	awg["protocol_version"] = version
	lastConfig, err := json.Marshal(client)
	if err != nil {
		return "", err
	}
	awg["last_config"] = string(lastConfig)
	profile := map[string]any{
		"hostName": host, "description": name,
		"defaultContainer": "amnezia-awg2",
		"containers":       []any{map[string]any{"container": "amnezia-awg2", "awg": awg}},
	}
	dns := iface.Key("DNS").Strings(",")
	if len(dns) > 0 {
		profile["dns1"] = strings.TrimSpace(dns[0])
	}
	if len(dns) > 1 {
		profile["dns2"] = strings.TrimSpace(dns[1])
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := binary.Write(&out, binary.BigEndian, uint32(len(raw))); err != nil {
		return "", err
	}
	compressed, err := zlib.NewWriterLevel(&out, 8)
	if err != nil {
		return "", err
	}
	if _, err := compressed.Write(raw); err != nil {
		return "", err
	}
	if err := compressed.Close(); err != nil {
		return "", err
	}
	return "vpn://" + base64.RawURLEncoding.EncodeToString(out.Bytes()), nil
}
