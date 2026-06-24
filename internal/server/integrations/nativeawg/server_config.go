package nativeawg

import (
	"encoding/json"
	"fmt"

	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/server/model"
)

func EditServerConfig(config model.AmneziaWGNodeConfig) (*amneziaproto.EditServerConfig, error) {
	if err := ValidateNodeConfig(config); err != nil {
		return nil, err
	}
	if config.ConfigVersionMS <= 0 {
		return nil, fmt.Errorf("amneziawg config version must be greater than zero")
	}
	var timings map[string]string
	if config.TimingsJSON != "" {
		if err := json.Unmarshal([]byte(config.TimingsJSON), &timings); err != nil {
			return nil, err
		}
	}
	return &amneziaproto.EditServerConfig{
		NodeId:   config.NodeID,
		Revision: uint64(config.ConfigVersionMS),
		ServerConfig: &amneziaproto.ServerConfig{
			Jc: uint32(config.JC), Jmin: uint32(config.JMin), Jmax: uint32(config.JMax),
			S1: uint32(config.S1), S2: uint32(config.S2), S3: uint32(config.S3), S4: uint32(config.S4),
			H1: config.H1, H2: config.H2, H3: config.H3, H4: config.H4,
			I1: config.I1, I2: config.I2, I3: config.I3, I4: config.I4, I5: config.I5,
			ServerPrivateKey:       config.ServerPrivateKey,
			HeaderProtectionKey:    config.HeaderProtectionKey,
			ContentPaddingAddition: config.ContentPaddingAddition,
			ServerIpv4:             config.ServerIPv4,
			ClientIpv4Cidr:         config.ClientIPv4CIDR,
			ListenPort:             uint32(config.ListenPort),
			Timings:                timings, RandomTrailers: config.RandomTrailers, DisableCookies: config.DisableCookies,
		},
	}, nil
}
