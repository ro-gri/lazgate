package provisioning

import (
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/text/language"
)

func normalizeServerIP(value string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || address.Zone() != "" {
		return "", errors.New("server IP must be an IPv4 or IPv6 literal without a port or zone")
	}
	return address.Unmap().String(), nil
}

func normalizeServerRegion(value string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(value))
	if code == "" {
		return "", nil
	}
	region, err := language.ParseRegion(code)
	if err != nil || len(code) != 2 || !region.IsCountry() {
		return "", errors.New("region must be a two-letter country code")
	}
	return region.String(), nil
}

// SupportedCountries uses the same CLDR country definitions as validation.
func SupportedCountries() []string {
	codes := []string{}
	for _, region := range language.Supported.Regions() {
		if region.IsCountry() && len(region.String()) == 2 {
			codes = append(codes, region.String())
		}
	}
	return codes
}
