package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/server/storage"
)

func TestBootstrapCreatesPersistentIsolatedNodeFixtures(t *testing.T) {
	root := t.TempDir()
	if err := run(root); err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(filepath.Join(root, "server", "runtime.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(env), "LAZ_SECRET_KEY='") {
		t.Fatal("missing generated secret")
	}
	db, err := store.OpenSQLite(filepath.Join(root, "server", "laz.db"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := db.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("nodes: %v", nodes)
	}
	db.Close()
	for _, node := range []string{"hysteria", "amnezia"} {
		raw, err := os.ReadFile(filepath.Join(root, node, "node.crt"))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			t.Fatal("invalid certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if cert.IsCA || len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
			t.Fatal("invalid agent certificate usage")
		}
		if _, err := os.Stat(filepath.Join(root, node, "agent.yaml")); err != nil {
			t.Fatal(err)
		}
	}
	agentConfig, err := os.ReadFile(filepath.Join(root, "amnezia", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agentConfig), "docker") || !strings.Contains(string(agentConfig), "service_name: lab-awg") {
		t.Fatal("Amnezia lab must use host-local tools, not Docker")
	}
	awgConfig, err := os.ReadFile(filepath.Join(root, "amnezia", "awg.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(awgConfig), "HeaderProtectionKey = ") {
		t.Fatal("AWG 3.1 header protection is missing")
	}
	if err := run(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "server", "runtime.env"))
	if err != nil || string(after) != string(env) {
		t.Fatal("bootstrap changed existing credentials")
	}
}

func TestBootstrapStopsOnPartialInitialization(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "server"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server", "laz.db"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(root); err == nil {
		t.Fatal("partial state silently overwritten")
	}
}

func TestServerCertificateHasComposeDNSAndLocalhost(t *testing.T) {
	raw, _, _, err := certificate("server", true)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	for _, name := range []string{"server", "localhost", "127.0.0.1"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
