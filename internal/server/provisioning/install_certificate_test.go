package provisioning

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInstallerCertificatePassesProvisioningValidation(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is required")
	}
	raw, err := os.ReadFile("../../../deploy/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	start := strings.Index(script, "openssl req -x509 ")
	if start < 0 {
		t.Fatal("installer certificate generation command missing")
	}
	end := strings.Index(script[start:], "\nchown ")
	if end < 0 {
		t.Fatal("installer certificate generation command terminator missing")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Execute the installer's actual command, not a second copy of its options.
	cmd := exec.Command("bash", "-ec", "root=$1; domain=laz.example; "+script[start:start+end], "installer-test", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate certificate: %v: %s", err, output)
	}
	cert, err := os.ReadFile(filepath.Join(root, "keys", "agent-grpc.crt"))
	if err != nil {
		t.Fatal(err)
	}
	installer := New(nil)
	installer.SetAgentServerCertPEM(string(cert))
	if err := installer.validateAgentServerTrustMaterial(); err != nil {
		t.Fatalf("installer generated an unusable server certificate: %v", err)
	}
}
