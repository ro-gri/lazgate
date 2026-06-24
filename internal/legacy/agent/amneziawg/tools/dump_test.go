package tools

import (
	"strings"
	"testing"
	"time"
)

const goldenDump = "server-private\tserver-public\t51820\toff\n" +
	"peer-public-1\tpsk-1\t198.51.100.10:12345\t10.8.0.2/32\t1700000000\t1024\t2048\t25\n" +
	"peer-public-2\t(none)\t(none)\t0.0.0.0/32,10.8.0.3/32\t0\t0\t8\toff\n"

func TestParseShowDumpHappyPath(t *testing.T) {
	dump, err := ParseShowDump(goldenDump)
	if err != nil {
		t.Fatal(err)
	}
	if dump.PublicKey != "server-public" || dump.ListenPort != 51820 || len(dump.Peers) != 2 {
		t.Fatalf("unexpected interface dump: %+v", dump)
	}
	first := dump.Peers[0]
	if first.Endpoint != "198.51.100.10:12345" || first.ReceiveBytes != 1024 || first.TransmitBytes != 2048 || first.PersistentKeepalive != 25 {
		t.Fatalf("unexpected first peer: %+v", first)
	}
	if got := first.LatestHandshake; !got.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("handshake = %v", got)
	}
	second := dump.Peers[1]
	if second.Endpoint != "" || second.PresharedKey != "(none)" || len(second.AllowedIPs) != 2 || second.PersistentKeepalive != 0 {
		t.Fatalf("unexpected second peer: %+v", second)
	}
}

func TestParseAWG2ExtendedInterfaceDump(t *testing.T) {
	header := strings.Join([]string{"private", "public", "51820", "4", "40", "70", "32", "48", "16", "16", "1001", "1002", "1003", "1004", "(null)", "(null)", "(null)", "(null)", "(null)", "0x1234"}, "\t")
	dump, err := ParseShowDump(header + "\npeer\t(none)\t(none)\t10.8.1.2/32\t0\t10\t20\t25\n")
	if err != nil {
		t.Fatal(err)
	}
	if dump.PublicKey != "public" || dump.FwMark != "0x1234" || dump.ListenPort != 51820 || len(dump.Peers) != 1 || dump.Peers[0].ReceiveBytes != 10 {
		t.Fatalf("unexpected AWG2 dump: %+v", dump)
	}
}

func TestParseAWG31Dump(t *testing.T) {
	// Field order from tools v3.1.20260812 src/show.c dump_print.
	fields := []string{"private", "public", "51820", "4", "40", "70", "32", "48", "16", "16", "1001-2000", "2001", "3001", "4001", "(null)", "(null)", "(null)", "(null)", "(null)", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", "0-32", "120-180", "5", "180", "10", "18", "on", "off", "0x1234"}
	dump, err := ParseShowDump(strings.Join(fields, "\t") + "\npeer\t(none)\t(none)\t10.8.1.2/32\t1700000000\t10\t20\t25-30\n")
	if err != nil {
		t.Fatal(err)
	}
	if dump.FwMark != "0x1234" || dump.ListenPort != 51820 || len(dump.Peers) != 1 || dump.Peers[0].PersistentKeepalive != 25 || dump.Peers[0].PersistentKeepaliveRange != "25-30" {
		t.Fatalf("dump=%+v", dump)
	}
}

func TestParseKeepaliveRejectsMalformedRanges(t *testing.T) {
	for _, value := range []string{"30-25", "1-2-3", "1-65536", "-1", "1-"} {
		if _, err := parseKeepalive(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestParseShowDumpDuplicatePublicKey(t *testing.T) {
	raw := "priv\tpub\t51820\toff\npeer\t(none)\t(none)\t10.0.0.2/32\t0\t0\t0\toff\npeer\t(none)\t(none)\t10.0.0.3/32\t0\t0\t0\toff\n"
	_, err := ParseShowDump(raw)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v, want duplicate", err)
	}
}

func TestParseShowDumpInvalidDump(t *testing.T) {
	cases := map[string]string{
		"missing interface fields": "priv\tpub\n",
		"bad allowed ip":           "priv\tpub\t51820\toff\npeer\t(none)\t(none)\tnot-cidr\t0\t0\t0\toff\n",
		"ipv6 allowed ip":          "priv\tpub\t51820\toff\npeer\t(none)\t(none)\t2001:db8::1/128\t0\t0\t0\toff\n",
		"bad handshake":            "priv\tpub\t51820\toff\npeer\t(none)\t(none)\t10.0.0.2/32\tbad\t0\t0\toff\n",
		"bad counter":              "priv\tpub\t51820\toff\npeer\t(none)\t(none)\t10.0.0.2/32\t0\t-1\t0\toff\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseShowDump(raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
