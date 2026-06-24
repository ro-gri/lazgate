package provisioning

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

const awgMinPrefix = 16
const awgMaxPrefix = 30

type awgSubnet struct {
	Network netip.Prefix
	Server  netip.Addr
	First   netip.Addr
	Last    netip.Addr
}

func parseAWGSubnet(value string) (awgSubnet, error) {
	network, err := netip.ParsePrefix(value)
	if err != nil || !network.Addr().Is4() || network.Bits() < awgMinPrefix || network.Bits() > awgMaxPrefix || network != network.Masked() || network.String() != value {
		return awgSubnet{}, errors.New("subnet must be an IPv4 network with prefix /16 through /30 and no host bits, for example 10.8.1.0/24")
	}
	base := network.Addr().As4()
	last := binary.BigEndian.Uint32(base[:]) + ((uint32(1) << (32 - network.Bits())) - 2)
	var lastBytes [4]byte
	binary.BigEndian.PutUint32(lastBytes[:], last)
	server := network.Addr().Next()
	return awgSubnet{Network: network, Server: server, First: server.Next(), Last: netip.AddrFrom4(lastBytes)}, nil
}
