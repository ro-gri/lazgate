package cps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/pion/stun/v3"
)

// This test-only expander checks the actual runtime bytes produced by the tags,
// rather than validating only the text emitted by the generator.
func expand(t *testing.T, signature Signature) []byte {
	t.Helper()
	tags := regexp.MustCompile(`<b 0x([0-9a-f]+)>|<(r|rc|rd) ([0-9]+)>`)
	var out []byte
	position := 0
	for _, match := range tags.FindAllStringSubmatchIndex(signature.CPS, -1) {
		if match[0] != position {
			t.Fatalf("unparsed CPS: %q", signature.CPS[position:match[0]])
		}
		if match[2] >= 0 {
			raw, err := hex.DecodeString(signature.CPS[match[2]:match[3]])
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, raw...)
		} else {
			n, err := strconv.Atoi(signature.CPS[match[6]:match[7]])
			if err != nil || n < 1 || n > MaxPacketSize {
				t.Fatal("invalid dynamic length")
			}
			raw := make([]byte, n)
			if _, err := rand.Read(raw); err != nil {
				t.Fatal(err)
			}
			kind := signature.CPS[match[4]:match[5]]
			alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
			if kind == "rd" {
				alphabet = "0123456789"
			}
			if kind != "r" {
				for i := range raw {
					raw[i] = alphabet[int(raw[i])%len(alphabet)]
				}
			}
			out = append(out, raw...)
		}
		position = match[1]
	}
	if position != len(signature.CPS) || len(out) != signature.Size || len(out) > MaxPacketSize {
		t.Fatalf("invalid expanded size %d: %+v", len(out), signature)
	}
	return out
}

func TestDNSChainsRemainValidAfterDynamicSubstitution(t *testing.T) {
	for _, service := range []Service{Google, Apple, Microsoft} {
		first, err := Generate(context.Background(), Options{Profile: DNS, Service: service})
		if err != nil {
			t.Fatal(err)
		}
		second, err := Generate(context.Background(), Options{Profile: DNS, Service: service})
		if err != nil {
			t.Fatal(err)
		}
		if first == second {
			t.Fatal("reused a static signature list")
		}
		seen := map[string]bool{}
		for _, signature := range first.Signatures {
			if !signature.Dynamic || seen[signature.CPS] {
				t.Fatal("missing variation")
			}
			seen[signature.CPS] = true
			var previous []byte
			for range 8 {
				raw := expand(t, signature)
				if bytes.Equal(raw, previous) {
					t.Fatal("runtime substitution did not vary packet")
				}
				previous = raw
				var message dns.Msg
				if err := message.Unpack(raw); err != nil {
					t.Fatal(err)
				}
				if message.Response || !message.RecursionDesired || len(message.Question) != 1 || !strings.EqualFold(message.Question[0].Name, dns.Fqdn(first.ServerName)) {
					t.Fatalf("invalid query: %+v", message)
				}
				opt := message.IsEdns0()
				if opt == nil || len(opt.Option) != 2 {
					t.Fatal("missing cookie and padding")
				}
				cookie := opt.Option[0].(*dns.EDNS0_COOKIE)
				if len(cookie.Cookie) != 16 {
					t.Fatal("wrong cookie length")
				}
				padding := opt.Option[1].(*dns.EDNS0_PADDING).Padding
				if len(padding) > 16 {
					t.Fatal("excessive DNS padding")
				}
				if !bytes.Equal(padding, make([]byte, len(padding))) {
					t.Fatal("DNS padding must stay zero")
				}
			}
		}
	}
}

func TestSTUNChainsRemainValidAfterDynamicSubstitution(t *testing.T) {
	var previous Chain
	for range 20 {
		chain, err := Generate(context.Background(), Options{Profile: STUN})
		if err != nil {
			t.Fatal(err)
		}
		if chain == previous {
			t.Fatal("reused STUN chain")
		}
		previous = chain
		for _, signature := range chain.Signatures {
			for range 5 {
				message := &stun.Message{Raw: expand(t, signature)}
				if err := message.Decode(); err != nil {
					t.Fatal(err)
				}
				if message.Type != stun.BindingRequest || message.Contains(stun.AttrFingerprint) || message.Contains(stun.AttrMessageIntegrity) {
					t.Fatal("invalid dynamic STUN message")
				}
				padding, err := message.Get(stun.AttrPadding)
				if err != nil {
					t.Fatal(err)
				}
				if len(padding) < 12 || len(padding) > 24 || len(padding)%4 != 0 {
					t.Fatalf("unexpected STUN padding size %d", len(padding))
				}
			}
		}
	}
}

func TestCompactSignaturesKeepVariableLengths(t *testing.T) {
	for _, profile := range []Profile{DNS, STUN} {
		options := Options{Profile: profile}
		if profile == DNS {
			options.ServerName = "fonts.gstatic.com"
		}
		sizes := map[int]bool{}
		for range 32 {
			chain, err := Generate(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			for _, signature := range chain.Signatures {
				sizes[signature.Size] = true
				if len(signature.CPS) > 200 {
					t.Fatalf("%s signature unnecessarily long: %d", profile, len(signature.CPS))
				}
			}
		}
		if len(sizes) < 2 {
			t.Fatalf("%s packet lengths do not vary", profile)
		}
	}
}

func TestOptionsAreStrict(t *testing.T) {
	for _, options := range []Options{
		{Profile: "tls"}, {Profile: DNS, Service: "other"}, {Profile: DNS, Service: Custom},
		{Profile: STUN, Service: Google}, {Profile: STUN, ServerName: "google.com"},
		{Profile: QUIC, Service: Google, ServerName: "apple.com"},
		{Profile: DNS, ServerName: "https://example.com"}, {Profile: QUIC, ServerName: "127.0.0.1"},
		{Profile: DNS, ServerName: "*.example.com"}, {Profile: DNS, ServerName: "bad\n.example.com"},
	} {
		if _, err := Generate(context.Background(), options); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("accepted %+v: %v", options, err)
		}
	}
	chain, err := Generate(context.Background(), Options{Profile: DNS, ServerName: "EXAMPLE.COM."})
	if err != nil || chain.Service != Custom || chain.ServerName != "example.com" {
		t.Fatalf("custom name: %+v %v", chain, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Generate(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := Generate(ctx, Options{Profile: QUIC}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("QUIC deadline: %v", err)
	}
}

func TestEncodingRejectsUnsafeSpans(t *testing.T) {
	for _, fields := range [][]dynamicField{
		{{-1, 1, "r"}}, {{0, 0, "r"}}, {{0, 11, "r"}}, {{4, 2, "r"}, {1, 2, "r"}}, {{0, 1, "t"}},
	} {
		if _, err := encode(make([]byte, 10), fields...); err == nil {
			t.Fatal("invalid span accepted")
		}
	}
	if _, err := encode(make([]byte, MaxPacketSize+1)); err == nil {
		t.Fatal("oversized packet accepted")
	}
}

func FuzzHostname(f *testing.F) {
	for _, seed := range []string{"example.com", "EXAMPLE.COM.", "пример.рф", "127.0.0.1", "https://example.com", "bad\nname", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		name, err := hostname(input)
		if err != nil {
			return
		}
		if name == "" || len(name) > 253 || name != strings.ToLower(name) {
			t.Fatalf("invalid normalized hostname: %q", name)
		}
		for _, label := range strings.Split(name, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				t.Fatal("invalid normalized label")
			}
		}
	})
}
