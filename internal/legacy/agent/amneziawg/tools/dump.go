package tools

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type InterfaceDump struct {
	PrivateKey string
	PublicKey  string
	ListenPort uint16
	FwMark     string
	Peers      []PeerDump
}

type PeerDump struct {
	PublicKey                string
	PresharedKey             string
	Endpoint                 string
	AllowedIPs               []string
	LatestHandshake          time.Time
	LatestHandshakeUnix      int64
	ReceiveBytes             uint64
	TransmitBytes            uint64
	PersistentKeepalive      int
	PersistentKeepaliveRange string
}

func ParseShowDump(raw string) (InterfaceDump, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out InterfaceDump
	lineNo := 0
	seenInterface := false
	seenPeers := map[string]struct{}{}
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if !seenInterface {
			if len(fields) != 4 && len(fields) != 20 && len(fields) != 29 {
				return InterfaceDump{}, fmt.Errorf("invalid interface dump line %d: expected 4, 20 or 29 fields, got %d", lineNo, len(fields))
			}
			port, err := parsePort(fields[2])
			if err != nil {
				return InterfaceDump{}, fmt.Errorf("invalid interface listen port on line %d: %w", lineNo, err)
			}
			// AWG3.1 adds nine protection/timing fields; fwmark remains last.
			out = InterfaceDump{PrivateKey: fields[0], PublicKey: fields[1], ListenPort: port, FwMark: fields[len(fields)-1]}
			seenInterface = true
			continue
		}
		if len(fields) != 8 {
			return InterfaceDump{}, fmt.Errorf("invalid peer dump line %d: expected 8 fields, got %d", lineNo, len(fields))
		}
		publicKey := fields[0]
		if publicKey == "" || publicKey == "(none)" {
			return InterfaceDump{}, fmt.Errorf("invalid peer public key on line %d", lineNo)
		}
		if _, ok := seenPeers[publicKey]; ok {
			return InterfaceDump{}, fmt.Errorf("duplicate peer public key on line %d", lineNo)
		}
		seenPeers[publicKey] = struct{}{}
		handshake, err := parseInt64(fields[4], "latest handshake")
		if err != nil {
			return InterfaceDump{}, fmt.Errorf("invalid peer latest handshake on line %d: %w", lineNo, err)
		}
		rx, err := parseUint64(fields[5], "receive bytes")
		if err != nil {
			return InterfaceDump{}, fmt.Errorf("invalid peer receive bytes on line %d: %w", lineNo, err)
		}
		tx, err := parseUint64(fields[6], "transmit bytes")
		if err != nil {
			return InterfaceDump{}, fmt.Errorf("invalid peer transmit bytes on line %d: %w", lineNo, err)
		}
		keepalive, err := parseKeepalive(fields[7])
		if err != nil {
			return InterfaceDump{}, fmt.Errorf("invalid peer persistent keepalive on line %d: %w", lineNo, err)
		}
		allowedIPs, err := parseAllowedIPs(fields[3])
		if err != nil {
			return InterfaceDump{}, fmt.Errorf("invalid peer allowed ips on line %d: %w", lineNo, err)
		}
		out.Peers = append(out.Peers, PeerDump{
			PublicKey:                publicKey,
			PresharedKey:             fields[1],
			Endpoint:                 parseNone(fields[2]),
			AllowedIPs:               allowedIPs,
			LatestHandshake:          unixTime(handshake),
			LatestHandshakeUnix:      handshake,
			ReceiveBytes:             rx,
			TransmitBytes:            tx,
			PersistentKeepalive:      keepalive,
			PersistentKeepaliveRange: fields[7],
		})
	}
	if err := scanner.Err(); err != nil {
		return InterfaceDump{}, err
	}
	if !seenInterface {
		return InterfaceDump{}, fmt.Errorf("empty dump")
	}
	return out, nil
}

func parsePort(value string) (uint16, error) {
	if value == "off" || value == "(none)" {
		return 0, nil
	}
	port, err := strconv.ParseUint(value, 10, 16)
	return uint16(port), err
}

func parseInt64(value, name string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return parsed, nil
}

func parseUint64(value, name string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return parsed, nil
}

func parseKeepalive(value string) (int, error) {
	if value == "off" {
		return 0, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid keepalive range")
	}
	parsed, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("keepalive must be off or a non-negative integer")
	}
	if len(parts) == 2 {
		upper, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil || upper < parsed {
			return 0, fmt.Errorf("invalid keepalive range")
		}
	}
	return int(parsed), nil
}

func parseAllowedIPs(value string) ([]string, error) {
	if value == "" || value == "(none)" {
		return nil, nil
	}
	return validateAllowedIPv4CIDRs(strings.Split(value, ","))
}

func validateAllowedIPv4CIDRs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		part := strings.TrimSpace(value)
		if part == "" {
			return nil, fmt.Errorf("empty allowed ip")
		}
		ip, _, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		if ip.To4() == nil {
			return nil, fmt.Errorf("allowed ip must be IPv4 CIDR")
		}
		out = append(out, part)
	}
	return out, nil
}

func parseNone(value string) string {
	if value == "(none)" {
		return ""
	}
	return value
}

func unixTime(seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}
