package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestServeCancellationStopsListenersAndWorkers(t *testing.T) {
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	agentListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer agentListener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan struct{})
	done := make(chan error, 1)
	httpServer := &http.Server{Addr: address, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		done <- serve(ctx, httpServer, grpc.NewServer(), agentListener, func(ctx context.Context) {
			<-ctx.Done()
			close(workerDone)
		})
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://" + address + "/stream")
		if err == nil {
			defer response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	select {
	case <-workerDone:
	default:
		t.Fatal("returned before worker stopped")
	}
	for _, addr := range []string{address, agentListener.Addr().String()} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			t.Fatalf("listener still open: %s", addr)
		}
	}
}

func TestServeListenFailureStopsWorkers(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	workerDone := make(chan struct{})
	err = serve(ctx, &http.Server{Addr: occupied.Addr().String(), ErrorLog: nil, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}, nil, nil, func(ctx context.Context) {
		<-ctx.Done()
		close(workerDone)
	})
	if err == nil {
		t.Fatal("expected listen error")
	}
	select {
	case <-workerDone:
	default:
		t.Fatal("returned before worker stopped")
	}
}
