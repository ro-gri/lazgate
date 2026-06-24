package common

import (
	"context"
	"log/slog"

	commonproto "laz/generated/nodeproto/agentcommon"

	"google.golang.org/grpc"
)

type CollectionIssue struct {
	Code    string
	Subject string
	Message string
}

type AuthLookup interface {
	GetConnectionIDByNativeAuthID(context.Context, string) (string, error)
}

type CollectionResult[T any] struct {
	Value  T
	Issues []CollectionIssue
}

type RuntimeSnapshot struct {
	Status string
}

type CredentialSyncer interface {
	RunOnce(context.Context, bool) error
}

type RuntimeCollector interface {
	Collect(context.Context) (CollectionResult[RuntimeSnapshot], error)
}

type ServerConfigReconciler[C ServerConfigCommand] interface {
	EditServerConfig(context.Context, C) *commonproto.ServerConfigApplyResult
}

type RuntimeExecutor interface {
	Execute(context.Context, *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult
}

type OpenStreamFunc[S any] func(context.Context, grpc.ClientConnInterface) (Stream[S], error)

type DelegateConfig[C ServerConfigCommand, S ServerMessage[C]] struct {
	Component          string
	OpenStream         OpenStreamFunc[S]
	Syncer             CredentialSyncer
	Reconciler         ServerConfigReconciler[C]
	Executor           RuntimeExecutor
	TelemetryCollector *TelemetryCollector
	RuntimeCollector   RuntimeCollector
	Logger             *slog.Logger
}

// Delegate implements the protocol-independent processor behavior. Concrete
// agents only supply their stream constructor and VPN-specific components.
type Delegate[C ServerConfigCommand, S ServerMessage[C]] struct {
	cfg DelegateConfig[C, S]
}

func NewDelegate[C ServerConfigCommand, S ServerMessage[C]](cfg DelegateConfig[C, S]) *Delegate[C, S] {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Delegate[C, S]{cfg: cfg}
}

func (d *Delegate[C, S]) OpenStream(ctx context.Context, conn grpc.ClientConnInterface) (Stream[S], error) {
	return d.cfg.OpenStream(ctx, conn)
}

func (d *Delegate[C, S]) SyncCredentials(ctx context.Context, _ SyncRequest) error {
	return d.cfg.Syncer.RunOnce(ctx, false)
}

func (d *Delegate[C, S]) CollectTelemetry(ctx context.Context) TelemetrySnapshot {
	return d.cfg.TelemetryCollector.Collect(ctx)
}

func (d *Delegate[C, S]) RuntimeStatus(ctx context.Context) (string, error) {
	result, err := d.cfg.RuntimeCollector.Collect(ctx)
	if err != nil {
		return "", err
	}
	d.logIssues("runtime_status", result.Issues)
	return result.Value.Status, nil
}

func (d *Delegate[C, S]) ApplyServerConfig(ctx context.Context, command C) *commonproto.ServerConfigApplyResult {
	return d.cfg.Reconciler.EditServerConfig(ctx, command)
}

func (d *Delegate[C, S]) ExecuteRuntimeCommand(ctx context.Context, command *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult {
	return d.cfg.Executor.Execute(ctx, command)
}

func (d *Delegate[C, S]) logIssues(event string, issues []CollectionIssue) {
	for _, issue := range issues {
		attrs := []any{"event", event, "component", d.cfg.Component, "code", issue.Code, "message", issue.Message}
		if issue.Subject != "" {
			attrs = append(attrs, "subject", issue.Subject)
		}
		d.cfg.Logger.Warn("agent collection issue", attrs...)
	}
}
