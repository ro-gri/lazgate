package transport

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"
)

const encryptedPrefix = "enc:v1:"

type SecureStore struct {
	inner Store
	aead  cipher.AEAD
}

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(key string) (*SecretBox, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	raw, err := secretKeyBytes(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func WrapSecrets(inner Store, key string) (Store, error) {
	box, err := NewSecretBox(key)
	if err != nil {
		return nil, err
	}
	if box == nil {
		return inner, nil
	}
	return &SecureStore{inner: inner, aead: box.aead}, nil
}

func secretKeyBytes(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	sum := sha256.Sum256([]byte(value))
	return sum[:], nil
}

func (b *SecretBox) Seal(raw []byte) []byte {
	if b == nil || len(raw) == 0 || strings.HasPrefix(string(raw), encryptedPrefix) {
		return raw
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	sealed := b.aead.Seal(nil, nonce, raw, nil)
	payload := append(nonce, sealed...)
	return []byte(encryptedPrefix + base64.RawStdEncoding.EncodeToString(payload))
}

func (s *SecureStore) seal(raw []byte) []byte {
	return (&SecretBox{aead: s.aead}).Seal(raw)
}

func (s *SecureStore) open(raw []byte) []byte {
	value := string(raw)
	if !strings.HasPrefix(value, encryptedPrefix) {
		return raw
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil || len(payload) < s.aead.NonceSize() {
		return nil
	}
	nonce := payload[:s.aead.NonceSize()]
	sealed := payload[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil
	}
	return plain
}

func (s *SecureStore) protect(msg Message) Message {
	msg.Payload = s.seal(msg.Payload)
	msg.ResultPayload = s.seal(msg.ResultPayload)
	return msg
}

func (s *SecureStore) reveal(msg Message) Message {
	msg.Payload = s.open(msg.Payload)
	msg.ResultPayload = s.open(msg.ResultPayload)
	return msg
}

func (s *SecureStore) Enqueue(ctx context.Context, msg Message) error {
	return s.inner.Enqueue(ctx, s.protect(msg))
}
func (s *SecureStore) EnqueueInbox(ctx context.Context, msg Message) error {
	return s.inner.EnqueueInbox(ctx, s.protect(msg))
}
func (s *SecureStore) LeasePending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	msgs, err := s.inner.LeasePending(ctx, actorID, limit, leaseFor)
	for i := range msgs {
		msgs[i] = s.reveal(msgs[i])
	}
	return msgs, err
}
func (s *SecureStore) LeaseInboxPending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	msgs, err := s.inner.LeaseInboxPending(ctx, actorID, limit, leaseFor)
	for i := range msgs {
		msgs[i] = s.reveal(msgs[i])
	}
	return msgs, err
}
func (s *SecureStore) MarkApplied(ctx context.Context, id string, result []byte) error {
	return s.inner.MarkApplied(ctx, id, s.seal(result))
}
func (s *SecureStore) MarkAcked(ctx context.Context, id string, result []byte) error {
	return s.inner.MarkAcked(ctx, id, s.seal(result))
}
func (s *SecureStore) MarkFailed(ctx context.Context, id string, errMsg string, retryAt time.Time) error {
	return s.inner.MarkFailed(ctx, id, errMsg, retryAt)
}
func (s *SecureStore) MarkExpired(ctx context.Context, id string, errMsg string) error {
	return s.inner.MarkExpired(ctx, id, errMsg)
}
func (s *SecureStore) IsProcessed(ctx context.Context, actorID string, messageID string) (ProcessedMessage, bool, error) {
	processed, ok, err := s.inner.IsProcessed(ctx, actorID, messageID)
	processed.Result = s.open(processed.Result)
	return processed, ok, err
}
func (s *SecureStore) RecordProcessed(ctx context.Context, actorID string, messageID string, typ string, status Status, result []byte, errMsg string) error {
	return s.inner.RecordProcessed(ctx, actorID, messageID, typ, status, s.seal(result), errMsg)
}
func (s *SecureStore) RequeueExpiredLeases(ctx context.Context, actorID string) error {
	return s.inner.RequeueExpiredLeases(ctx, actorID)
}
func (s *SecureStore) Cleanup(ctx context.Context, policy CleanupPolicy) error {
	return s.inner.Cleanup(ctx, policy)
}
func (s *SecureStore) Close() error { return s.inner.Close() }
