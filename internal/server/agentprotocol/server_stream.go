package agentprotocol

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	ErrAgentNodeNotConnected = errors.New("agent node is not connected")
	ErrAgentSessionReplaced  = errors.New("agent session was replaced")
)

type ServerStreamIO[In any, Out any] interface {
	Recv() (In, error)
	Send(Out) error
	Context() context.Context
}

type HelloIdentity struct {
	NodeID         string
	InstallationID string
	AgentType      string
}

type InstallationIdentity struct {
	ID     string
	NodeID string
}

type ServerRouter[In any, Out any] interface {
	ParseHello(In) (HelloIdentity, bool)
	AuthenticateInstallation(context.Context, HelloIdentity) (InstallationIdentity, error)
	RequestID(In) string
	HandleMessage(context.Context, *StreamNode[In, Out], In)
}

type ServerNodeTasker[In any, Out any] interface {
	RunNodeTasks(context.Context, *StreamNode[In, Out]) error
}

type StreamHub[In any, Out any] struct {
	mu    sync.RWMutex
	nodes map[string]*StreamNode[In, Out]
}

type StreamNode[In any, Out any] struct {
	nodeID         string
	installationID string
	cancel         context.CancelFunc
	done           chan struct{}
	closeOnce      sync.Once
	send           chan Out
	incoming       chan In
	pending        map[string]chan In
	mu             sync.Mutex
}

func NewStreamHub[In any, Out any]() *StreamHub[In, Out] {
	return &StreamHub[In, Out]{nodes: map[string]*StreamNode[In, Out]{}}
}

func NewStreamNode[In any, Out any](nodeID string, installationID string, cancel context.CancelFunc, sendBuffer int, incomingBuffer int) *StreamNode[In, Out] {
	if sendBuffer <= 0 {
		sendBuffer = 64
	}
	if incomingBuffer <= 0 {
		incomingBuffer = 128
	}
	return &StreamNode[In, Out]{
		nodeID:         nodeID,
		installationID: installationID,
		cancel:         cancel,
		done:           make(chan struct{}),
		send:           make(chan Out, sendBuffer),
		incoming:       make(chan In, incomingBuffer),
		pending:        map[string]chan In{},
	}
}

func (n *StreamNode[In, Out]) NodeID() string {
	if n == nil {
		return ""
	}
	return n.nodeID
}

func (n *StreamNode[In, Out]) InstallationID() string {
	if n == nil {
		return ""
	}
	return n.installationID
}

func (n *StreamNode[In, Out]) Done() <-chan struct{} {
	if n == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return n.done
}

func (n *StreamNode[In, Out]) Outgoing() <-chan Out {
	if n == nil {
		return nil
	}
	return n.send
}

func (n *StreamNode[In, Out]) Send(msg Out) {
	if n == nil {
		return
	}
	select {
	case n.send <- msg:
	case <-n.done:
	default:
	}
}

func (n *StreamNode[In, Out]) close() {
	if n == nil {
		return
	}
	if n.cancel != nil {
		n.cancel()
	}
	n.closeOnce.Do(func() { close(n.done) })
}

func (h *StreamHub[In, Out]) Connect(stream ServerStreamIO[In, Out], router ServerRouter[In, Out]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello, ok := router.ParseHello(first)
	if !ok || hello.NodeID == "" || hello.InstallationID == "" || hello.AgentType == "" {
		return errors.New("agent hello with installation identity required")
	}
	installation, err := router.AuthenticateInstallation(stream.Context(), hello)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	node := NewStreamNode[In, Out](installation.NodeID, installation.ID, cancel, 64, 128)
	h.Register(installation.NodeID, node)
	defer h.Unregister(installation.NodeID, node)

	errc := make(chan error, 3)
	go h.writeLoop(ctx, stream, node, errc)
	go h.readLoop(ctx, stream, node, errc)
	go h.processLoop(ctx, router, node)
	if tasker, ok := router.(ServerNodeTasker[In, Out]); ok {
		go func() {
			if err := tasker.RunNodeTasks(ctx, node); err != nil {
				errc <- err
			}
		}()
	}

	select {
	case err = <-errc:
	case <-node.done:
		return nil
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (h *StreamHub[In, Out]) Request(ctx context.Context, nodeID string, requestID string, msg Out) (In, error) {
	var zero In
	node := h.Get(nodeID)
	if node == nil {
		return zero, ErrAgentNodeNotConnected
	}
	ch := make(chan In, 1)
	node.mu.Lock()
	node.pending[requestID] = ch
	node.mu.Unlock()
	defer func() {
		node.mu.Lock()
		delete(node.pending, requestID)
		node.mu.Unlock()
	}()
	select {
	case node.send <- msg:
	case <-node.done:
		return zero, ErrAgentSessionReplaced
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	waitCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		waitCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
	}
	defer cancel()
	select {
	case res := <-ch:
		return res, nil
	case <-waitCtx.Done():
		return zero, waitCtx.Err()
	}
}

func (h *StreamHub[In, Out]) Register(nodeID string, node *StreamNode[In, Out]) {
	h.mu.Lock()
	previous := h.nodes[nodeID]
	h.nodes[nodeID] = node
	h.mu.Unlock()
	if previous != nil {
		previous.close()
	}
}

func (h *StreamHub[In, Out]) Unregister(nodeID string, node *StreamNode[In, Out]) {
	h.mu.Lock()
	if h.nodes[nodeID] == node {
		delete(h.nodes, nodeID)
	}
	h.mu.Unlock()
	node.closeOnce.Do(func() { close(node.done) })
}

func (h *StreamHub[In, Out]) Get(nodeID string) *StreamNode[In, Out] {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.nodes[nodeID]
}

func (h *StreamHub[In, Out]) HandleIncoming(ctx context.Context, router ServerRouter[In, Out], node *StreamNode[In, Out], msg In) {
	requestID := router.RequestID(msg)
	if requestID != "" {
		node.mu.Lock()
		ch := node.pending[requestID]
		node.mu.Unlock()
		if ch != nil {
			ch <- msg
			return
		}
	}
	router.HandleMessage(ctx, node, msg)
}

func (h *StreamHub[In, Out]) writeLoop(ctx context.Context, stream ServerStreamIO[In, Out], node *StreamNode[In, Out], errc chan<- error) {
	for {
		select {
		case msg := <-node.send:
			if err := stream.Send(msg); err != nil {
				errc <- err
				return
			}
		case <-ctx.Done():
			return
		case <-node.done:
			return
		}
	}
}

func (h *StreamHub[In, Out]) readLoop(ctx context.Context, stream ServerStreamIO[In, Out], node *StreamNode[In, Out], errc chan<- error) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			errc <- err
			return
		}
		select {
		case node.incoming <- msg:
		case <-ctx.Done():
			return
		case <-node.done:
			return
		}
	}
}

func (h *StreamHub[In, Out]) processLoop(ctx context.Context, router ServerRouter[In, Out], node *StreamNode[In, Out]) {
	for {
		select {
		case msg := <-node.incoming:
			h.HandleIncoming(ctx, router, node, msg)
		case <-ctx.Done():
			return
		case <-node.done:
			return
		}
	}
}
