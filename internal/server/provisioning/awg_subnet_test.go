package provisioning

import (
	"fmt"
	"testing"
)

func TestAWGSubnetPools(t *testing.T) {
	for _, tc := range []struct{ cidr, server, first, last string }{
		{"10.8.0.0/16", "10.8.0.1", "10.8.0.2", "10.8.255.254"},
		{"10.8.16.0/20", "10.8.16.1", "10.8.16.2", "10.8.31.254"},
		{"10.8.0.0/23", "10.8.0.1", "10.8.0.2", "10.8.1.254"},
		{"10.8.1.0/24", "10.8.1.1", "10.8.1.2", "10.8.1.254"},
		{"10.8.1.128/25", "10.8.1.129", "10.8.1.130", "10.8.1.254"},
		{"10.8.1.252/30", "10.8.1.253", "10.8.1.254", "10.8.1.254"},
		{"255.255.255.252/30", "255.255.255.253", "255.255.255.254", "255.255.255.254"},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			pool, err := parseAWGSubnet(tc.cidr)
			if err != nil {
				t.Fatal(err)
			}
			if pool.Server.String() != tc.server || pool.First.String() != tc.first || pool.Last.String() != tc.last {
				t.Fatalf("wrong pool: %+v", pool)
			}
		})
	}
	for prefix := 16; prefix <= 30; prefix++ {
		if _, err := parseAWGSubnet(fmt.Sprintf("10.8.0.0/%d", prefix)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAWGSubnetRejectsInvalidNetworks(t *testing.T) {
	for _, value := range []string{"", "foo", "10.8.1.0", "10.8.1.256/24", "010.8.1.0/24", "10.8.1.1/24", "10.8.1.0/16", "10.8.1.128/24", "10.8.0.0/15", "10.8.1.0/31", "10.8.1.0/32", "::1/24", "::ffff:10.8.1.0/120", "10.8.1.0/024", "10.8.1.0/24\n"} {
		if _, err := parseAWGSubnet(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
