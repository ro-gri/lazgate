package provisioning

import (
	"fmt"
	"strings"
)

type awgPackage struct {
	Version string
	URL     string
	SHA256  string
}

type awgPlatform struct {
	Release string
	Tools   awgPackage
	DKMS    awgPackage
}

// Published PPA packages are pinned independently for each Ubuntu release.
var awgPlatforms = []awgPlatform{
	{Release: "24.04", Tools: awgPackage{
		Version: "1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1",
		URL:     "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/pool/main/a/amneziawg/amneziawg-tools_1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1_amd64.deb",
		SHA256:  "2c924076be2ba217eea04f9693291f01c5307a9be5aeee60d04525a50ff2804f",
	}, DKMS: awgPackage{
		Version: "1.0.0-0~202609061402+4569c4c~ubuntu24.04.1",
		URL:     "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/pool/main/a/amneziawg-linux-kmod/amneziawg-dkms_1.0.0-0~202609061402+4569c4c~ubuntu24.04.1_all.deb",
		SHA256:  "fadd07d0c2f9e89b61db713e37ba8fb05b5a15c067d13338bed3745193fa30be",
	}},
	{Release: "26.04", Tools: awgPackage{
		Version: "1.0.20210914-0~202609140833+ee0f0a9~ubuntu26.04.1",
		URL:     "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/pool/main/a/amneziawg/amneziawg-tools_1.0.20210914-0~202609140833+ee0f0a9~ubuntu26.04.1_amd64.deb",
		SHA256:  "b4832405e4e82c02ce5cc70151ee02c23f1a9cfe3ab078b8c5f3579a9213e4e0",
	}, DKMS: awgPackage{
		Version: "1.0.0-0~202609140848+4569c4c~ubuntu26.04.1",
		URL:     "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/pool/main/a/amneziawg-linux-kmod/amneziawg-dkms_1.0.0-0~202609140848+4569c4c~ubuntu26.04.1_all.deb",
		SHA256:  "4f42ace1e325783fe0402073e634cd208f85729a6c983d8c78b8525ae2ec74a5",
	}},
}

func awgSelectPlatformScript() string {
	var out strings.Builder
	out.WriteString(". /etc/os-release\ncase \"$ID:$VERSION_ID:$(dpkg --print-architecture)\" in\n")
	for _, platform := range awgPlatforms {
		fmt.Fprintf(&out, "ubuntu:%s:amd64)\n", platform.Release)
		for _, entry := range []struct {
			name string
			pkg  awgPackage
		}{{"tools", platform.Tools}, {"dkms", platform.DKMS}} {
			fmt.Fprintf(&out, "%s_version=%s\n%s_url=%s\n%s_sha256=%s\n", entry.name, shellQuote(entry.pkg.Version), entry.name, shellQuote(entry.pkg.URL), entry.name, shellQuote(entry.pkg.SHA256))
		}
		out.WriteString(";;\n")
	}
	out.WriteString("*) echo UNSUPPORTED_OS >&2; exit 1;;\nesac\n")
	return out.String()
}
