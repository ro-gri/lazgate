package transport

import (
	"context"
	"time"
)

type Direction string

const (
	DirectionOutbound Direction = "outbound"
	DirectionInbound  Direction = "inbound"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusInFlight   Status = "in_flight"
	StatusApplied    Status = "applied"
	StatusAcked      Status = "acked"
	StatusFailed     Status = "failed"
	StatusExpired    Status = "expired"
	StatusSuperseded Status = "superseded"
)

type Message struct {
	ID            string
	ActorID       string
	Direction     Direction
	Type          string
	Payload       []byte
	Status        Status
	Attempts      int
	AvailableAt   time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SentAt        time.Time
	ProcessedAt   time.Time
	ResultPayload []byte
	Error         string
}

type CleanupPolicy struct {
	AuthAckedTTL       time.Duration
	AuthSupersededTTL  time.Duration
	AuthFailedTTL      time.Duration
	RuntimeAckedTTL    time.Duration
	RuntimeFailedTTL   time.Duration
	ProcessedTTL       time.Duration
	OnlineAckedTTL     time.Duration
	OnlineFailedTTL    time.Duration
	TrafficAckedTTL    time.Duration
	TrafficFailedTTL   time.Duration
	DefaultAckedTTL    time.Duration
	DefaultFailedTTL   time.Duration
	DefaultExpiredTTL  time.Duration
	PayloadRedactAfter time.Duration
}

func DefaultCleanupPolicy() CleanupPolicy {
	return CleanupPolicy{
		AuthAckedTTL:       15 * time.Minute,
		AuthSupersededTTL:  5 * time.Minute,
		AuthFailedTTL:      time.Hour,
		RuntimeAckedTTL:    time.Hour,
		RuntimeFailedTTL:   24 * time.Hour,
		ProcessedTTL:       24 * time.Hour,
		OnlineAckedTTL:     5 * time.Minute,
		OnlineFailedTTL:    15 * time.Minute,
		TrafficAckedTTL:    time.Hour,
		TrafficFailedTTL:   7 * 24 * time.Hour,
		DefaultAckedTTL:    time.Hour,
		DefaultFailedTTL:   24 * time.Hour,
		DefaultExpiredTTL:  time.Hour,
		PayloadRedactAfter: 24 * time.Hour,
	}
}

type Store interface {
	Enqueue(ctx context.Context, msg Message) error
	LeasePending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error)
	MarkSent(ctx context.Context, id string) error
	MarkApplied(ctx context.Context, id string, result []byte) error
	MarkAcked(ctx context.Context, id string, result []byte) error
	MarkFailed(ctx context.Context, id string, errMsg string, retryAt time.Time) error
	MarkExpired(ctx context.Context, id string, errMsg string) error
	IsProcessed(ctx context.Context, actorID string, messageID string) (ProcessedMessage, bool, error)
	RecordProcessed(ctx context.Context, actorID string, messageID string, typ string, status Status, result []byte, errMsg string) error
	RequeueExpiredLeases(ctx context.Context, actorID string) error
	Cleanup(ctx context.Context, policy CleanupPolicy) error
	Close() error
}

type ProcessedMessage struct {
	ActorID     string
	MessageID   string
	Type        string
	Status      Status
	Result      []byte
	Error       string
	ProcessedAt time.Time
}

type NopStore struct{}

func (NopStore) Enqueue(context.Context, Message) error { return nil }
func (NopStore) LeasePending(context.Context, string, int, time.Duration) ([]Message, error) {
	return nil, nil
}
func (NopStore) MarkSent(context.Context, string) error                      { return nil }
func (NopStore) MarkApplied(context.Context, string, []byte) error           { return nil }
func (NopStore) MarkAcked(context.Context, string, []byte) error             { return nil }
func (NopStore) MarkFailed(context.Context, string, string, time.Time) error { return nil }
func (NopStore) MarkExpired(context.Context, string, string) error           { return nil }
func (NopStore) IsProcessed(context.Context, string, string) (ProcessedMessage, bool, error) {
	return ProcessedMessage{}, false, nil
}
func (NopStore) RecordProcessed(context.Context, string, string, string, Status, []byte, string) error {
	return nil
}
func (NopStore) RequeueExpiredLeases(context.Context, string) error { return nil }
func (NopStore) Cleanup(context.Context, CleanupPolicy) error       { return nil }
func (NopStore) Close() error                                       { return nil }
