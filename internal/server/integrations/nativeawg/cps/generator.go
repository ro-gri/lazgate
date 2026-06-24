// Package cps generates offline AmneziaWG signature chains. It neither changes
// VPN settings nor contacts the services named in a generated packet.
package cps

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"strings"

	"golang.org/x/net/idna"
)

const PacketCount = 5
const MaxPacketSize = 1232 // Fits a minimum-MTU IPv6 path including UDP/IP headers.

type Profile string

const (
	Auto Profile = "auto"
	DNS  Profile = "dns"
	STUN Profile = "stun"
	QUIC Profile = "quic"
)

type Service string

const (
	ServiceAuto Service = "auto"
	Google      Service = "google"
	Apple       Service = "apple"
	Microsoft   Service = "microsoft"
	Custom      Service = "custom"
)

type Options struct {
	Profile Profile
	Service Service
	// ServerName is an optional DNS name / QUIC SNI, never a dial destination.
	ServerName string
}

type Signature struct {
	CPS     string
	Size    int
	Dynamic bool
}

type Chain struct {
	Profile    Profile
	Service    Service
	ServerName string
	Signatures [PacketCount]Signature
}

var ErrInvalidOptions = errors.New("invalid CPS generator options")

var serviceNames = map[Service][]string{
	Google:    {"www.google.com", "accounts.google.com", "fonts.gstatic.com"},
	Apple:     {"www.apple.com", "gateway.icloud.com", "captive.apple.com"},
	Microsoft: {"www.microsoft.com", "login.microsoftonline.com", "outlook.office.com"},
}

// Generate creates a fresh five-packet chain. For QUIC these are Initial
// transmissions of ONE handshake, not five independently encrypted handshakes.
func Generate(ctx context.Context, options Options) (Chain, error) {
	var result Chain
	if err := ctx.Err(); err != nil {
		return result, err
	}
	options, err := normalize(options)
	if err != nil {
		return result, err
	}
	result.Profile, result.Service, result.ServerName = options.Profile, options.Service, options.ServerName
	if options.Profile == QUIC {
		result.Signatures, err = generateQUIC(ctx, options.ServerName)
		if err != nil {
			return Chain{}, err
		}
		return result, err
	}
	for i := range result.Signatures {
		if err := ctx.Err(); err != nil {
			return Chain{}, err
		}
		switch options.Profile {
		case DNS:
			result.Signatures[i], err = generateDNS(options.ServerName, i)
		case STUN:
			result.Signatures[i], err = generateSTUN()
		}
		if err != nil {
			return Chain{}, err
		}
	}
	return result, nil
}

func normalize(options Options) (Options, error) {
	if options.Profile == "" {
		options.Profile = Auto
	}
	if options.Service == "" {
		options.Service = ServiceAuto
	}
	if options.Profile != Auto && options.Profile != DNS && options.Profile != STUN && options.Profile != QUIC {
		return options, fmt.Errorf("%w: unknown profile", ErrInvalidOptions)
	}
	if options.Service != ServiceAuto && options.Service != Custom {
		if _, ok := serviceNames[options.Service]; !ok {
			return options, fmt.Errorf("%w: unknown service", ErrInvalidOptions)
		}
	}
	if options.Profile == Auto {
		profiles := []Profile{DNS, STUN, QUIC}
		if options.Service != ServiceAuto || options.ServerName != "" {
			profiles = []Profile{DNS, QUIC}
		}
		i, err := randomInt(len(profiles))
		if err != nil {
			return options, err
		}
		options.Profile = profiles[i]
	}
	if options.Profile == STUN {
		if options.Service != ServiceAuto || options.ServerName != "" {
			return options, fmt.Errorf("%w: generic STUN has no SNI or service identity", ErrInvalidOptions)
		}
		options.Service = ""
		return options, nil
	}
	if options.ServerName != "" {
		if options.Service != ServiceAuto && options.Service != Custom {
			return options, fmt.Errorf("%w: custom server name requires custom service", ErrInvalidOptions)
		}
		name, err := hostname(options.ServerName)
		if err != nil {
			return options, err
		}
		options.ServerName = name
		options.Service = Custom
		return options, nil
	}
	if options.Service == Custom {
		return options, fmt.Errorf("%w: custom service requires a server name", ErrInvalidOptions)
	}
	if options.Service == ServiceAuto {
		services := []Service{Google, Apple, Microsoft}
		i, err := randomInt(len(services))
		if err != nil {
			return options, err
		}
		options.Service = services[i]
	}
	names := serviceNames[options.Service]
	i, err := randomInt(len(names))
	if err != nil {
		return options, err
	}
	options.ServerName = names[i]
	return options, nil
}

func hostname(value string) (string, error) {
	if len(value) > 1024 {
		return "", fmt.Errorf("%w: DNS name too long", ErrInvalidOptions)
	}
	name, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if err != nil || name == "" || len(name) > 253 {
		return "", fmt.Errorf("%w: invalid DNS name", ErrInvalidOptions)
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return "", fmt.Errorf("%w: server name must not be an IP address", ErrInvalidOptions)
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%w: invalid DNS label", ErrInvalidOptions)
		}
		for _, c := range strings.ToLower(label) {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("%w: invalid DNS label", ErrInvalidOptions)
			}
		}
	}
	return strings.ToLower(name), nil
}

func randomInt(limit int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, fmt.Errorf("CPS entropy: %w", err)
	}
	return int(n.Int64()), nil
}
