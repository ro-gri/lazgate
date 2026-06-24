package audit

import (
	"context"
	"encoding/json"

	"laz/internal/model"
	adminauthsvc "laz/internal/services/adminauth"
)

type Store interface {
	CreateAuditLog(model.AuditLog) (model.AuditLog, error)
}

type Recorder struct {
	store Store
}

type Event struct {
	Action     string
	EntityType string
	EntityID   string
	Details    any
}

func New(st Store) *Recorder {
	return &Recorder{store: st}
}

func (r *Recorder) Record(ctx context.Context, event Event) (model.AuditLog, error) {
	principal, ok := adminauthsvc.PrincipalFromContext(ctx)
	actor := "unknown"
	if ok && principal.Name != "" {
		actor = principal.Name
	}
	details, err := detailsJSON(event.Details)
	if err != nil {
		return model.AuditLog{}, err
	}
	return r.store.CreateAuditLog(model.AuditLog{
		Actor:      actor,
		Action:     event.Action,
		EntityType: event.EntityType,
		EntityID:   event.EntityID,
		Details:    details,
	})
}

func detailsJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
