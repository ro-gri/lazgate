package common

import (
	"context"
	"errors"
	"testing"

	"laz/internal/protocol"
)

type memCursor struct {
	value int64
	ok    bool
}

func (m *memCursor) LoadSyncCursor(context.Context, string) (int64, bool, error) {
	return m.value, m.ok, nil
}

func (m *memCursor) SaveSyncCursor(_ context.Context, _ string, cursorMS int64) error {
	m.value = cursorMS
	m.ok = true
	return nil
}

type testSource struct {
	pages []protocol.Response[string]
	reqs  []protocol.Request
}

func (s *testSource) FetchSyncPage(_ context.Context, req protocol.Request) (protocol.Response[string], error) {
	s.reqs = append(s.reqs, req)
	if len(s.pages) == 0 {
		return protocol.Response[string]{ManifestStartedAtMS: 1}, nil
	}
	page := s.pages[0]
	s.pages = s.pages[1:]
	return page, nil
}

type testApplier struct {
	applied []string
	fail    bool
	finish  PassResult
}

func (a *testApplier) BeginSyncPass(context.Context, bool) (Pass[string], error) {
	return a, nil
}

func (a *testApplier) ApplySyncChange(_ context.Context, change protocol.Change[string]) error {
	if a.fail {
		return errors.New("apply failed")
	}
	a.applied = append(a.applied, change.ConnectionID)
	return nil
}

func (a *testApplier) FinishSyncPass(_ context.Context, result PassResult) error {
	a.finish = result
	return nil
}

func TestEngineCommitsFirstPageCursorAfterTrailingRequest(t *testing.T) {
	cursor := &memCursor{value: 100, ok: true}
	source := &testSource{pages: []protocol.Response[string]{
		{ManifestStartedAtMS: 200, Changes: []protocol.Change[string]{{ConnectionID: "a", Operation: protocol.OperationUpsert, Payload: "A"}}},
		{ManifestStartedAtMS: 300, Changes: []protocol.Change[string]{}},
	}}
	applier := &testApplier{}
	err := Engine[string]{CursorKey: "cursor", PageSize: 1, Cursors: cursor, Source: source, Applier: applier}.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.value != 200 {
		t.Fatalf("cursor = %d, want first page manifest 200", cursor.value)
	}
	if len(source.reqs) != 2 || source.reqs[1].FromConnectionID != "a" {
		t.Fatalf("unexpected page requests: %#v", source.reqs)
	}
	if _, ok := applier.finish.SeenConnectionIDs["a"]; !ok {
		t.Fatalf("expected seen credential in finish result: %#v", applier.finish.SeenConnectionIDs)
	}
}

func TestEngineDoesNotCommitCursorAfterApplyFailure(t *testing.T) {
	cursor := &memCursor{value: 100, ok: true}
	source := &testSource{pages: []protocol.Response[string]{
		{ManifestStartedAtMS: 200, Changes: []protocol.Change[string]{{ConnectionID: "a", Operation: protocol.OperationUpsert}}},
	}}
	err := Engine[string]{CursorKey: "cursor", PageSize: 100, Cursors: cursor, Source: source, Applier: &testApplier{fail: true}}.Run(context.Background(), false)
	if err == nil {
		t.Fatal("expected apply error")
	}
	if cursor.value != 100 {
		t.Fatalf("cursor advanced after failure: %d", cursor.value)
	}
}
