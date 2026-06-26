package admin

import (
	"context"
	"encoding/json"
	"time"

	"laz/internal/server/model"
	provisioningsvc "laz/internal/server/provisioning"
)

const (
	operationRunning model.Status = "running"
	operationDone    model.Status = "done"
	stepPending      model.Status = "pending"
	stepRunning      model.Status = "running"
	stepDone         model.Status = "done"
)

func (a *App) publish(ctx context.Context, event model.Event) model.Event {
	if a.events == nil {
		return event
	}
	return a.events.Publish(ctx, event)
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func operationItem(op model.Operation) map[string]any {
	return map[string]any{
		"id":          op.ID,
		"type":        op.Type,
		"status":      op.Status,
		"entity_type": op.EntityType,
		"entity_id":   op.EntityID,
		"summary":     op.Summary,
		"result":      decodeObject(op.ResultJSON),
		"error":       op.Error,
		"created_at":  op.CreatedAt,
		"updated_at":  op.UpdatedAt,
	}
}

func operationStepItem(step model.OperationStep) map[string]any {
	return map[string]any{
		"id":           step.ID,
		"operation_id": step.OperationID,
		"seq":          step.Seq,
		"name":         step.Name,
		"type":         step.Type,
		"status":       step.Status,
		"message":      step.Message,
		"result":       decodeObject(step.ResultJSON),
		"error":        step.Error,
		"created_at":   step.CreatedAt,
		"started_at":   zeroTimeNil(step.StartedAt),
		"completed_at": zeroTimeNil(step.CompletedAt),
		"updated_at":   zeroTimeNil(step.UpdatedAt),
	}
}

func operationStepItems(steps []model.OperationStep) []map[string]any {
	out := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		out = append(out, operationStepItem(step))
	}
	return out
}

func decodeObject(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func zeroTimeNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func stepStatus(status provisioningsvc.StepStatus) model.Status {
	switch status {
	case provisioningsvc.StepRunning:
		return stepRunning
	case provisioningsvc.StepOK:
		return stepDone
	case provisioningsvc.StepFailed:
		return model.StatusError
	default:
		return stepPending
	}
}
