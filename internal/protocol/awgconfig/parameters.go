package awgconfig

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var Timings = []string{"RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts"}

func FormatBool(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

var rangePattern = regexp.MustCompile(`^(\d+)(?:-(\d+))?$`)
var cpsPattern = regexp.MustCompile(`<b 0x([0-9a-fA-F]+)>|<(r|rc|rd) (\d+)>|<t>`)

func Range(value string) (uint64, uint64, error) {
	m := rangePattern.FindStringSubmatch(value)
	if m == nil {
		return 0, 0, fmt.Errorf("expected uint32 or uint32 range")
	}
	a, e := strconv.ParseUint(m[1], 10, 32)
	if e != nil {
		return 0, 0, fmt.Errorf("invalid uint32 range")
	}
	b := a
	if m[2] != "" {
		b, e = strconv.ParseUint(m[2], 10, 32)
	}
	if e != nil || b < a {
		return 0, 0, fmt.Errorf("invalid uint32 range")
	}
	return a, b, nil
}

func Validate(headers [4]string, signatures [5]string, padding string, timings map[string]string) error {
	var ranges [4][2]uint64
	for i, value := range headers {
		a, b, err := Range(value)
		if err != nil {
			return fmt.Errorf("H%d: %w", i+1, err)
		}
		ranges[i] = [2]uint64{a, b}
		for j := 0; j < i; j++ {
			if a <= ranges[j][1] && ranges[j][0] <= b {
				return fmt.Errorf("H%d overlaps H%d", i+1, j+1)
			}
		}
	}
	if padding != "" && padding != "(off)" {
		if _, high, err := Range(padding); err != nil || high > 65535 {
			return fmt.Errorf("ContentPaddingAddition must be in 0..65535")
		}
	}
	for key, value := range timings {
		allowed := false
		for _, name := range Timings {
			if key == name {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("unknown timing parameter")
		}
		if value != "" && value != "(off)" {
			if _, high, err := Range(value); err != nil || high > 65535 {
				return fmt.Errorf("%s must be in 0..65535", key)
			}
		}
	}
	for i, value := range signatures {
		if err := ValidateCPS(value); err != nil {
			return fmt.Errorf("I%d: %w", i+1, err)
		}
	}
	return nil
}

func ValidateCPS(value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("CPS must be a single line")
	}
	if value == "" {
		return nil
	}
	if len(value) > 150000 {
		return fmt.Errorf("CPS string is too long")
	}
	end, size := 0, uint64(0)
	for _, m := range cpsPattern.FindAllStringSubmatchIndex(value, -1) {
		if strings.TrimSpace(value[end:m[0]]) != "" {
			return fmt.Errorf("invalid CPS tag")
		}
		if m[2] >= 0 {
			n := m[3] - m[2]
			if n%2 != 0 {
				return fmt.Errorf("CPS hex must contain whole bytes")
			}
			size += uint64(n / 2)
		} else if m[4] >= 0 {
			n, err := strconv.ParseUint(value[m[6]:m[7]], 10, 16)
			if err != nil || n == 0 {
				return fmt.Errorf("invalid CPS random length")
			}
			size += n
		} else {
			size += 4
		}
		if size > 65507 {
			return fmt.Errorf("CPS exceeds maximum UDP payload")
		}
		end = m[1]
	}
	if end == 0 || strings.TrimSpace(value[end:]) != "" {
		return fmt.Errorf("invalid CPS syntax")
	}
	return nil
}
