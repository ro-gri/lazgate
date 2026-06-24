package tools

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

type hostNetworkConfig struct {
	Address    string
	ListenPort int
}

// Keep awg-quick's boot configuration and owned firewall rules aligned with the
// runtime. Only LazGate's rules are removed; unrelated host rules are untouched.
func (f *Facade) applyHostNetwork(ctx context.Context, config hostNetworkConfig) error {
	if _, err := f.runtime.Run(ctx, f.execArgv("ip", "address", "replace", config.Address, "dev", f.iface), nil); err != nil {
		return err
	}
	if f.hostConfigPath == "" {
		return nil
	}
	raw, err := os.ReadFile(f.hostConfigPath)
	if err != nil {
		return fmt.Errorf("read awg-quick config: %w", err)
	}
	cfg, err := ini.LoadSources(snapshotOptions, raw)
	if err != nil {
		return err
	}
	section := cfg.Section("Interface")
	oldAddress := section.Key("Address").String()
	prefix, err := netip.ParsePrefix(config.Address)
	if err != nil {
		return err
	}
	network := prefix.Masked().String()
	port := fmt.Sprint(config.ListenPort)
	run := func(binary string, args ...string) error {
		if f.useSudo {
			args = append([]string{"-n", binary}, args...)
			binary = "sudo"
		}
		_, err := f.runtime.Run(ctx, append([]string{binary}, args...), nil)
		return err
	}
	// MASQUERADE uses the routed egress automatically; do not cache a NIC name.
	rules := [][]string{
		{"INPUT", "-p", "udp", "--dport", port, "-m", "comment", "--comment", "lazgate-awg", "-j", "ACCEPT"},
		{"FORWARD", "-i", f.iface, "-s", network, "-m", "comment", "--comment", "lazgate-awg", "-j", "ACCEPT"},
		{"FORWARD", "-o", f.iface, "-d", network, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-m", "comment", "--comment", "lazgate-awg", "-j", "ACCEPT"},
		{"POSTROUTING", "-s", network, "-m", "comment", "--comment", "lazgate-awg", "-j", "MASQUERADE"},
	}
	for _, rule := range rules {
		var table []string
		if rule[0] == "POSTROUTING" {
			table = []string{"-t", "nat"}
		}
		if err := run("iptables", append(append([]string{"-w", "5"}, table...), append([]string{"-C"}, rule...)...)...); err != nil {
			if err := run("iptables", append(append([]string{"-w", "5"}, table...), append([]string{"-I"}, rule...)...)...); err != nil {
				return err
			}
		}
	}
	if oldAddress != "" && oldAddress != config.Address {
		old, err := netip.ParsePrefix(oldAddress)
		if err != nil {
			return fmt.Errorf("invalid previous VPN address")
		}
		args := []string{"ip", "-4", "-o", "address", "show", "dev", f.iface}
		if f.useSudo {
			args = append([]string{"sudo", "-n"}, args...)
		}
		result, err := f.runtime.Run(ctx, args, nil)
		if err != nil {
			return err
		}
		if slices.Contains(strings.Fields(result.Stdout), old.String()) {
			if err := run("ip", "address", "del", old.String(), "dev", f.iface); err != nil {
				return err
			}
		}
	}
	// Build fixed, non-user-supplied hooks for the next awg-quick restart.
	var up, down []string
	for _, rule := range rules {
		table := ""
		if rule[0] == "POSTROUTING" {
			table = "-t nat "
		}
		up = append(up, "iptables "+table+"-I "+strings.Join(rule, " "))
		down = append(down, "iptables "+table+"-D "+strings.Join(rule, " "))
	}
	for key, value := range map[string]string{"Address": config.Address, "PostUp": strings.Join(up, "; "), "PostDown": strings.Join(down, "; ")} {
		section.DeleteKey(key)
		section.Key(key).SetValue(value)
	}
	section.DeleteKey("SaveConfig")
	if err := atomicSnapshot(f.hostConfigPath, encodeSnapshot(cfg)); err != nil {
		return err
	}
	for _, table := range []string{"filter", "nat"} {
		args := []string{"iptables", "-w", "5", "-t", table, "-S"}
		if f.useSudo {
			args = append([]string{"sudo", "-n"}, args...)
		}
		result, err := f.runtime.Run(ctx, args, nil)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			parts := strings.Fields(line)
			for i := range parts {
				parts[i] = strings.Trim(parts[i], `"`)
			}
			if len(parts) < 3 || parts[0] != "-A" {
				continue
			}
			owned := false
			for i := 0; i+1 < len(parts); i++ {
				if parts[i] == "--comment" && parts[i+1] == "lazgate-awg" {
					owned = true
				}
			}
			if !owned {
				continue
			}
			flag := func(key string) string {
				for i := 0; i+1 < len(parts); i++ {
					if parts[i] == key {
						return parts[i+1]
					}
				}
				return ""
			}
			// iptables -S reorders arguments and adds implicit modules (e.g. -m udp).
			keep := parts[1] == "INPUT" && flag("--dport") == port ||
				parts[1] == "FORWARD" && flag("-i") == f.iface && flag("-s") == network ||
				parts[1] == "FORWARD" && flag("-o") == f.iface && flag("-d") == network ||
				parts[1] == "POSTROUTING" && flag("-s") == network && flag("-o") == ""
			if !keep {
				parts[0] = "-D"
				if err := run("iptables", append([]string{"-w", "5", "-t", table}, parts...)...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *Facade) saveHostConfig(ctx context.Context) error {
	if f.hostConfigPath == "" {
		return nil
	}
	_, err := f.runtime.RunSecretOutput(ctx, f.execArgv("awg-quick", "save", f.hostConfigPath))
	return err
}
