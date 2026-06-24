package cps

import (
	"net"
	"os"
	"sync"
	"time"
)

// captureConn is an offline net.PacketConn adapter for quic-go. It opens no
// socket and never receives a server response, so all writes are Initial flights
// (including loss-recovery transmissions) of the same client handshake.
type captureConn struct {
	packets       chan []byte
	closed        chan struct{}
	wake          chan struct{}
	once          sync.Once
	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

func newCaptureConn() *captureConn {
	return &captureConn{packets: make(chan []byte, PacketCount+2), closed: make(chan struct{}), wake: make(chan struct{}, 1)}
}

func (c *captureConn) ReadFrom([]byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		deadline := c.readDeadline
		c.mu.Unlock()
		var timer *time.Timer
		var expiry <-chan time.Time
		if !deadline.IsZero() {
			if !time.Now().Before(deadline) {
				return 0, nil, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			expiry = timer.C
		}
		select {
		case <-c.closed:
			if timer != nil {
				timer.Stop()
			}
			return 0, nil, net.ErrClosed
		case <-expiry:
			return 0, nil, os.ErrDeadlineExceeded
		case <-c.wake:
			if timer != nil {
				timer.Stop()
			}
		}
	}
}

func (c *captureConn) WriteTo(packet []byte, _ net.Addr) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	c.mu.Lock()
	deadline := c.writeDeadline
	c.mu.Unlock()
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	select {
	case c.packets <- append([]byte(nil), packet...):
	default: // The caller only needs the first five transmissions.
	}
	return len(packet), nil
}

func (c *captureConn) Close() error { c.once.Do(func() { close(c.closed) }); return nil }
func (c *captureConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 49152}
}
func (c *captureConn) SetReadBuffer(int) error  { return nil }
func (c *captureConn) SetWriteBuffer(int) error { return nil }
func (c *captureConn) SetReadDeadline(value time.Time) error {
	c.mu.Lock()
	c.readDeadline = value
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}
func (c *captureConn) SetWriteDeadline(value time.Time) error {
	c.mu.Lock()
	c.writeDeadline = value
	c.mu.Unlock()
	return nil
}
func (c *captureConn) SetDeadline(value time.Time) error {
	_ = c.SetReadDeadline(value)
	return c.SetWriteDeadline(value)
}
