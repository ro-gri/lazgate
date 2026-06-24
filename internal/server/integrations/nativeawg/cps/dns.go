package cps

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/miekg/dns"
)

func generateDNS(name string, index int) (Signature, error) {
	// DNS names are case-insensitive: vary the query without inventing hostnames.
	bits := make([]byte, len(name))
	if _, err := rand.Read(bits); err != nil {
		return Signature{}, err
	}
	letters := []byte(name)
	for i, c := range letters {
		if c >= 'a' && c <= 'z' && bits[i]&1 != 0 {
			letters[i] -= 'a' - 'A'
		}
	}
	message := new(dns.Msg)
	types := []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeHTTPS}
	message.SetQuestion(dns.Fqdn(string(letters)), types[index%len(types)])
	message.SetEdns0(1232, false)
	cookie := make([]byte, 8)
	if _, err := rand.Read(cookie); err != nil {
		return Signature{}, err
	}
	message.IsEdns0().Option = []dns.EDNS0{&dns.EDNS0_COOKIE{Code: dns.EDNS0COOKIE, Cookie: hex.EncodeToString(cookie)}}
	beforePadding, err := message.Pack()
	if err != nil {
		return Signature{}, err
	}
	cookieOffset := len(beforePadding) - len(cookie)
	paddingSize, err := randomInt(17)
	if err != nil {
		return Signature{}, err
	}
	// RFC 7830 recommends zero padding; do not replace it with random bytes.
	message.IsEdns0().Option = append(message.IsEdns0().Option, &dns.EDNS0_PADDING{Padding: make([]byte, paddingSize)})
	packet, err := message.Pack()
	if err != nil {
		return Signature{}, err
	}
	if !bytes.Equal(packet[cookieOffset:cookieOffset+8], cookie) {
		return Signature{}, fmt.Errorf("unexpected DNS cookie layout")
	}
	var check dns.Msg
	if err := check.Unpack(packet); err != nil || len(check.Question) != 1 || !strings.EqualFold(check.Question[0].Name, dns.Fqdn(name)) {
		return Signature{}, fmt.Errorf("invalid generated DNS query")
	}
	// The ID and client cookie have no message checksum or authentication field.
	return encode(packet, dynamicField{0, 2, "r"}, dynamicField{cookieOffset, 8, "r"})
}
