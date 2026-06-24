package cps

import (
	"encoding/hex"
	"fmt"
	"strings"
)

type dynamicField struct {
	offset int
	length int
	tag    string
}

// Encode only known-safe variable spans. Protocol lengths remain library-built;
// authenticated or checksummed bytes must never be marked dynamic.
func encode(packet []byte, fields ...dynamicField) (Signature, error) {
	if len(packet) == 0 || len(packet) > MaxPacketSize {
		return Signature{}, fmt.Errorf("CPS packet size %d is outside 1..%d", len(packet), MaxPacketSize)
	}
	var text strings.Builder
	position := 0
	static := func(raw []byte) {
		// Keep individual hex literals small enough for older client parsers.
		for len(raw) > 0 {
			n := min(len(raw), 480)
			text.WriteString("<b 0x" + hex.EncodeToString(raw[:n]) + ">")
			raw = raw[n:]
		}
	}
	for _, field := range fields {
		if field.offset < position || field.length < 1 || field.offset+field.length > len(packet) {
			return Signature{}, fmt.Errorf("invalid CPS dynamic span")
		}
		if field.tag != "r" && field.tag != "rc" && field.tag != "rd" {
			return Signature{}, fmt.Errorf("unsupported CPS dynamic tag")
		}
		static(packet[position:field.offset])
		fmt.Fprintf(&text, "<%s %d>", field.tag, field.length)
		position = field.offset + field.length
	}
	static(packet[position:])
	return Signature{CPS: text.String(), Size: len(packet), Dynamic: len(fields) > 0}, nil
}
