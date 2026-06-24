package workqueue

import (
	"context"
	"errors"
	"laz/internal/queue"
	"laz/internal/server/model"
	"testing"
)

type testCompleter struct {
	calls   int
	message queue.Message
	result  Result
}

func (c *testCompleter) Complete(_ context.Context, m queue.Message, r Result) error {
	c.calls++
	c.message = m
	c.result = r
	return nil
}
func TestWorkerRequiresCompleter(t *testing.T) {
	if w, err := New(queue.NopStore{}, nil, "server", nil); err == nil || w != nil {
		t.Fatal("missing completer accepted")
	}
	if w, err := New(nil, &testCompleter{}, "server", nil); err == nil || w != nil {
		t.Fatal("missing queue accepted")
	}
}
func TestWorkerCompletesSuccessAtomically(t *testing.T) {
	completer := &testCompleter{}
	worker, err := New(queue.NopStore{}, completer, "server", nil)
	if err != nil {
		t.Fatal(err)
	}
	worker.Register("test", HandlerFunc(func(context.Context, queue.Message) (Result, error) {
		return Result{Next: []queue.Message{{ID: "next"}}, Events: []model.Event{{Type: "done"}}, Output: []byte("ok")}, nil
	}))
	worker.handle(context.Background(), queue.Message{ID: "current", Type: "test"})
	if completer.calls != 1 || completer.message.ID != "current" || len(completer.result.Events) != 1 || len(completer.result.Next) != 1 || completer.result.Next[0].ActorID != "server" || completer.result.Next[0].Status != queue.StatusPending {
		t.Fatalf("completion: %+v", completer)
	}
}
func TestWorkerCompletesFailureWithRetryAndEvents(t *testing.T) {
	completer := &testCompleter{}
	worker, err := New(queue.NopStore{}, completer, "server", nil)
	if err != nil {
		t.Fatal(err)
	}
	worker.Register("test", HandlerFunc(func(context.Context, queue.Message) (Result, error) {
		return Result{Events: []model.Event{{Type: "failed"}}}, errors.New("unavailable")
	}))
	worker.handle(context.Background(), queue.Message{ID: "current", Type: "test"})
	if completer.calls != 1 || completer.result.Status != queue.StatusFailed || completer.result.RetryAt.IsZero() || string(completer.result.Output) != "unavailable" || len(completer.result.Events) != 1 {
		t.Fatalf("completion: %+v", completer)
	}
}
