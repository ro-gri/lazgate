package nativeawg

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"laz/internal/server/model"
)

func TestVPNURLPreservesClientConfigurationAndAWG31(t *testing.T) {
	config := completeNodeConfig()
	config.HeaderProtectionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	config.I1, config.I2, config.I3, config.I4, config.I5 = "", "", "", "", ""
	config.ContentPaddingAddition = "32-128"
	config.DNS = "1.1.1.1, 8.8.8.8"
	conf, err := RenderClientConf(config, model.AmneziaWGConnectionMaterial{
		ClientPrivateKey: "client-private", PresharedKey: "preshared", AssignedIPv4: "10.8.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	uri, err := RenderVPNURL("My phone", conf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "vpn://") || strings.Contains(uri, "=") {
		t.Fatal("expected unpadded vpn URI")
	}
	frame, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(uri, "vpn://"))
	if err != nil || len(frame) <= 4 {
		t.Fatalf("invalid frame: %v", err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(frame[4:]))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(frame[:4]) != uint32(len(raw)) {
		t.Fatal("qCompress length header mismatch")
	}
	var profile struct {
		Description string `json:"description"`
		Host        string `json:"hostName"`
		Container   string `json:"defaultContainer"`
		DNS1        string `json:"dns1"`
		DNS2        string `json:"dns2"`
		Containers  []struct {
			Container string         `json:"container"`
			AWG       map[string]any `json:"awg"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Description != "My phone" || profile.Host != config.EndpointHost ||
		profile.Container != "amnezia-awg2" || len(profile.Containers) != 1 ||
		profile.DNS1 != "1.1.1.1" || profile.DNS2 != "8.8.8.8" {
		t.Fatal("unexpected Amnezia profile metadata")
	}
	awg := profile.Containers[0].AWG
	if awg["protocol_version"] != "3.1" || awg["isThirdPartyConfig"] != true ||
		awg["HeaderProtectionKey"] != config.HeaderProtectionKey {
		t.Fatal("missing AWG 3.1 server settings")
	}
	var client map[string]any
	if err := json.Unmarshal([]byte(awg["last_config"].(string)), &client); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"config": conf, "client_priv_key": "client-private", "psk_key": "preshared",
		"server_pub_key": config.ServerPublicKey, "client_ip": "10.8.1.2/32",
		"HeaderProtectionKey": config.HeaderProtectionKey, "ContentPaddingAddition": "32-128",
		"port": float64(51820), "hostName": config.EndpointHost,
	} {
		if client[key] != want {
			t.Fatalf("field %s was not preserved", key)
		}
	}
	if strings.Contains(string(raw), config.ServerPrivateKey) || strings.Contains(string(raw), "userName") ||
		strings.Contains(string(raw), "server_priv_key") {
		t.Fatal("server administration secrets leaked into client export")
	}
}

func TestVPNURLRejectsMalformedClientConfiguration(t *testing.T) {
	for _, conf := range []string{"", "[Interface]\nPrivateKey = secret\n", "[Interface]\n[Peer]\nEndpoint = host:not-port"} {
		if _, err := RenderVPNURL("name", conf); err == nil {
			t.Fatal("malformed configuration accepted")
		}
	}
	conf, err := RenderClientConf(completeNodeConfig(), model.AmneziaWGConnectionMaterial{
		ClientPrivateKey: "private", PresharedKey: "psk", AssignedIPv4: "10.8.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RenderVPNURL("name", conf+"\n[Peer]\nPublicKey = second\n"); err == nil {
		t.Fatal("multiple peers accepted")
	}
}
