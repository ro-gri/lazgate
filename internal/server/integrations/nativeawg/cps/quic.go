package cps

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	quic "github.com/quic-go/quic-go"
)

func generateQUIC(parent context.Context, name string) ([PacketCount]Signature, error) {
	var result [PacketCount]Signature
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	sink := newCaptureConn()
	variation, err := randomInt(3)
	if err != nil {
		return result, err
	}
	transport := &quic.Transport{Conn: sink, ConnectionIDLength: 8 + variation*4}
	done := make(chan struct{})
	var dialErr error
	go func() {
		defer close(done)
		_, dialErr = transport.Dial(ctx, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 443},
			&tls.Config{ServerName: name, MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"}, CurvePreferences: []tls.CurveID{tls.X25519}},
			&quic.Config{Versions: []quic.Version{quic.Version1}, HandshakeIdleTimeout: 10 * time.Second, InitialPacketSize: uint16(1200 + 16*variation), DisablePathMTUDiscovery: true})
	}()
	defer func() {
		cancel()
		_ = sink.Close()
		_ = transport.Close()
		// Transport close plus context cancellation terminate the dial goroutine.
		<-done
	}()
	for i := range result {
		select {
		case packet := <-sink.packets:
			if len(packet) < 1200 || packet[0]&0xc0 != 0xc0 || binary.BigEndian.Uint32(packet[1:5]) != 1 {
				return result, fmt.Errorf("QUIC stack produced a non-Initial datagram")
			}
			result[i], err = encode(packet)
			if err != nil {
				return result, err
			}
		case <-ctx.Done():
			return result, ctx.Err()
		case <-done:
			return result, fmt.Errorf("QUIC Initial generation ended early: %w", dialErr)
		}
	}
	return result, nil
}
