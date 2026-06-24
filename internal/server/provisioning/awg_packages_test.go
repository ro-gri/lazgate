package provisioning

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAWGPackageProfiles(t *testing.T) {
	seen := map[string]bool{}
	for _, platform := range awgPlatforms {
		if seen[platform.Release] {
			t.Fatal("duplicate platform")
		}
		seen[platform.Release] = true
		for _, pkg := range []awgPackage{platform.Tools, platform.DKMS} {
			hash, err := hex.DecodeString(pkg.SHA256)
			if err != nil || len(hash) != 32 {
				t.Fatal("invalid SHA256")
			}
			if !strings.HasPrefix(pkg.URL, "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/pool/") || !strings.Contains(pkg.URL, pkg.Version) || !strings.Contains(pkg.Version, "ubuntu"+platform.Release+".1") {
				t.Fatal("mixed-release package profile")
			}
		}
	}
	if len(seen) != 2 || !seen["24.04"] || !seen["26.04"] {
		t.Fatal("supported Ubuntu releases changed")
	}
	launch := awgLaunchScript(AWGState{})
	if strings.Contains(launch, "./dkms.deb ./tools.deb") || !strings.Contains(launch, "--no-install-recommends -y ./tools.deb") {
		t.Fatal("DKMS must remain under the upstream installer's compatibility repair flow")
	}
}

// Opt-in because these tests download official images/packages and run apt.
// No privilege, host mount, kernel module load or existing Compose change.
func TestAWGPackagesInUbuntuContainers(t *testing.T) {
	if os.Getenv("LAZ_TEST_AWG_PACKAGES") != "1" {
		t.Skip("set LAZ_TEST_AWG_PACKAGES=1 to verify Ubuntu package installation in Docker")
	}
	for _, platform := range awgPlatforms {
		t.Run(platform.Release, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			name := fmt.Sprintf("lazgate-awg-packages-%s-%d", strings.ReplaceAll(platform.Release, ".", ""), time.Now().UnixNano())
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
			}()
			script := `set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install --no-install-recommends -y -qq ca-certificates curl >/dev/null
` + awgSelectPlatformScript() + `
fetch() { curl --proto '=https' --tlsv1.2 -fsSL --retry 2 --max-time 120 "$1" -o "$2"; printf '%s  %s\n' "$3" "$2" | sha256sum -c -; }
fetch "$tools_url" /tmp/tools.deb "$tools_sha256"
fetch "$dkms_url" /tmp/dkms.deb "$dkms_sha256"
test "$(dpkg-deb -f /tmp/tools.deb Version)" = "$tools_version"
test "$(dpkg-deb -f /tmp/dkms.deb Version)" = "$dkms_version"
apt-get install --no-install-recommends -y -qq /tmp/tools.deb >/dev/null
awg --version | grep -F v3.1.
test -x /usr/bin/awg-quick
test ! -e /var/lib/dkms/amneziawg
# Inspect source membership without executing DKMS or installing its files.
dpkg-deb --fsys-tarfile /tmp/dkms.deb | tar -tf - > /tmp/module-contents
grep -E '^\./usr/src/[^/]+/dkms.conf$' /tmp/module-contents
`
			cmd := exec.CommandContext(ctx, "docker", "run", "--name", name, "--rm", "-i", "--platform", "linux/amd64", "ubuntu:"+platform.Release, "sh", "-s")
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.CombinedOutput()
			if err != nil {
				if len(out) > 6000 {
					out = out[len(out)-6000:]
				}
				t.Fatalf("Ubuntu %s package check: %v\n%s", platform.Release, err, out)
			}
			t.Logf("Ubuntu %s amd64: tools installed/executed, tools and DKMS archives SHA256/version verified; DKMS not loaded", platform.Release)
		})
	}
}
