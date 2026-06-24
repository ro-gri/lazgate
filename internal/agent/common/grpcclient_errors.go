package common

import "errors"

var ErrMissingRequestID = errors.New("gRPC request_id is required")

var ErrMissingTLSConfig = errors.New("gRPC TLS config builder is required")

var ErrEmptyTLSConfig = errors.New("gRPC TLS config builder returned nil")

var ErrMissingAuthManifestResponse = errors.New("gRPC response does not contain an auth manifest")

var ErrMissingUsageAck = errors.New("gRPC response does not contain a usage acknowledgement")

var ErrMismatchedUsageAckRequestID = errors.New("gRPC usage acknowledgement request_id mismatch")

var ErrDisconnected = errors.New("gRPC client is disconnected")
