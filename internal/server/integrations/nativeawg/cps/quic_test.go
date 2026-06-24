package cps

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
)

func initialIDs(t *testing.T, raw []byte) ([]byte, []byte) {
	t.Helper()
	if len(raw) < 1200 || binary.BigEndian.Uint32(raw[1:5]) != 1 {
		t.Fatal("not a padded QUIC v1 Initial")
	}
	n := int(raw[5])
	if n == 0 || 6+n >= len(raw) {
		t.Fatal("invalid destination connection ID")
	}
	dest := raw[6 : 6+n]
	start := 6 + n
	n = int(raw[start])
	start++
	if n == 0 || start+n >= len(raw) {
		t.Fatal("invalid source connection ID")
	}
	return dest, raw[start : start+n]
}

func TestQUICChainIsOneRealHandshake(t *testing.T) {
	chain, err := Generate(context.Background(), Options{Profile: QUIC, Service: Google})
	if err != nil {
		t.Fatal(err)
	}
	var first, dest, source []byte
	seen := map[string]bool{}
	for i, signature := range chain.Signatures {
		if signature.Dynamic {
			t.Fatal("protected QUIC data must not use runtime CPS mutations")
		}
		raw := expand(t, signature)
		d, s := initialIDs(t, raw)
		if i == 0 {
			first, dest, source = raw, d, s
		}
		if !bytes.Equal(d, dest) || !bytes.Equal(s, source) {
			t.Fatal("mixed independent QUIC connections")
		}
		if seen[string(raw)] {
			t.Fatal("duplicate QUIC transmission")
		}
		seen[string(raw)] = true
	}

	// A real QUIC server decrypts the Initial and parses its TLS ClientHello.
	// Only this interoperability TEST uses loopback UDP, never the generator.
	names := make(chan string, 1)
	listener, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			select {
			case names <- hello.ServerName:
			default:
			}
			return nil, errors.New("stop after validating ClientHello")
		},
	}, &quic.Config{Versions: []quic.Version{quic.Version1}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := net.Dial("udp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(first); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-names:
		if name != chain.ServerName {
			t.Fatalf("SNI %q != %q", name, chain.ServerName)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("QUIC server did not accept the generated Initial")
	}
	second, err := Generate(context.Background(), Options{Profile: QUIC, Service: Google})
	if err != nil {
		t.Fatal(err)
	}
	if chain == second {
		t.Fatal("reused QUIC chain")
	}
}

func TestCaptureReadCancellation(t *testing.T) {
	c := newCaptureConn()
	done := make(chan error, 1)
	go func() { _, _, err := c.ReadFrom(nil); done <- err }()
	_ = c.SetReadDeadline(time.Now())
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing deadline error")
		}
	case <-time.After(time.Second):
		t.Fatal("reader leaked")
	}
	_ = c.Close()
	if _, err := c.WriteTo([]byte("data"), nil); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}
