package agentprotocol

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type InstallationRecord struct {
	ID        string
	NodeID    string
	AgentType string
}

type InstallationLookup func(fingerprint string) (InstallationRecord, error)

type InstallationRecordStore interface {
	LookupActiveAgentInstallation(fingerprint string) (InstallationRecord, error)
}

func NewInstallationLookup(store InstallationRecordStore) InstallationLookup {
	return func(fingerprint string) (InstallationRecord, error) {
		if store == nil {
			return InstallationRecord{}, errors.New("agent installation store is required")
		}
		return store.LookupActiveAgentInstallation(fingerprint)
	}
}

func AuthenticateMTLSInstallation(ctx context.Context, hello HelloIdentity, expectedAgentType string, lookup InstallationLookup) (InstallationIdentity, error) {
	if lookup == nil {
		return InstallationIdentity{}, errors.New("agent installation lookup is required")
	}
	p, ok := peer.FromContext(ctx)
	if !ok {
		return InstallationIdentity{}, errors.New("agent peer is missing")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return InstallationIdentity{}, errors.New("agent client certificate is missing")
	}
	leaf := tlsInfo.State.PeerCertificates[0]
	if leaf.IsCA || !leaf.NotBefore.Before(time.Now()) || !leaf.NotAfter.After(time.Now()) {
		return InstallationIdentity{}, errors.New("agent client certificate is not currently valid")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(leaf.Raw))
	installation, err := lookup(fingerprint)
	if err != nil {
		return InstallationIdentity{}, errors.New("unknown, revoked, or inactive agent certificate")
	}
	if installation.ID != hello.InstallationID || installation.NodeID != hello.NodeID || installation.AgentType != hello.AgentType {
		return InstallationIdentity{}, errors.New("agent hello installation identity does not match certificate")
	}
	if installation.AgentType != expectedAgentType {
		return InstallationIdentity{}, errors.New("agent installation type is not allowed on this service")
	}
	return InstallationIdentity{ID: installation.ID, NodeID: installation.NodeID}, nil
}
