package clientauth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"laz/internal/server/connections"
	"laz/internal/server/model"
	"laz/internal/server/storage"
)

type fakeConnectionCreator struct{}

func (fakeConnectionCreator) CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, client model.Client, trafficLimitGB, expirationDays int) (connections.ConnectionResult, error) {
	return connections.ConnectionResult{
		Connection: model.Connection{
			ID:        "acc_fake",
			AccountID: account.ID,
			ClientID:  client.ID,
			NodeID:    node.ID,
			Protocol:  connections.EnrollmentProtocolForNode(node),
			Status:    model.StatusActive,
		},
		Configs: []model.IssuedConfig{{ID: "cfg_fake"}},
	}, nil
}

func (fakeConnectionCreator) DeleteConnection(ctx context.Context, connection model.Connection, node model.Node) (model.Connection, error) {
	connection.Status = model.StatusDeleted
	return connection, nil
}

func TestSetupLoginAndAuthenticateSession(t *testing.T) {
	st, account := newFixtures(t)
	svc := New(st, fakeConnectionCreator{})

	setup, err := svc.SetupPIN(SetupPINInput{AccountID: account.ID, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if setup.RecoveryCode == "" {
		t.Fatal("expected recovery code")
	}

	login, err := svc.Login(LoginInput{Username: account.Username, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if login.SessionToken == "" {
		t.Fatal("expected session token")
	}
	if login.Session.Token != "" {
		t.Fatal("did not expect raw session token to be stored")
	}

	session, authenticatedUser, err := svc.AuthenticateSession(login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if session.AccountID != account.ID || authenticatedUser.ID != account.ID {
		t.Fatalf("unexpected session/account: %+v %+v", session, authenticatedUser)
	}
}

func TestLoginUsesAccountIDWhenUsernamesDuplicate(t *testing.T) {
	st, first := newFixtures(t)
	second, err := st.CreateAccount(model.Account{Username: first.Username, DisplayName: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, fakeConnectionCreator{})
	if _, err := svc.SetupPIN(SetupPINInput{AccountID: first.ID, PIN: "111111"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetupPIN(SetupPINInput{AccountID: second.ID, PIN: "222222"}); err != nil {
		t.Fatal(err)
	}

	login, err := svc.Login(LoginInput{AccountID: second.ID, PIN: "222222"})
	if err != nil {
		t.Fatal(err)
	}
	if login.Account.ID != second.ID {
		t.Fatalf("expected second account, got %+v", login.Account)
	}
}

func TestLoginLockout(t *testing.T) {
	st, account := newFixtures(t)
	svc := New(st, fakeConnectionCreator{})
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.SetupPIN(SetupPINInput{AccountID: account.ID, PIN: "123456"}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < maxFailedAttempts; i++ {
		_, _ = svc.Login(LoginInput{Username: account.Username, PIN: "bad"})
	}
	_, err := svc.Login(LoginInput{Username: account.Username, PIN: "123456"})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected lockout, got %v", err)
	}
}

func TestExpiredLoginLockoutResetsAttempts(t *testing.T) {
	st, account := newFixtures(t)
	svc := New(st, fakeConnectionCreator{})
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.SetupPIN(SetupPINInput{AccountID: account.ID, PIN: "123456"}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < maxFailedAttempts; i++ {
		_, _ = svc.Login(LoginInput{Username: account.Username, PIN: "bad"})
	}
	now = now.Add(lockoutDuration + time.Second)
	if _, err := svc.Login(LoginInput{Username: account.Username, PIN: "bad"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials after expired lockout, got %v", err)
	}
	credential, err := st.GetClientCredential(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.FailedAttempts != 1 || !credential.LockedUntil.IsZero() {
		t.Fatalf("expected reset attempts after expired lockout, got attempts=%d locked_until=%v", credential.FailedAttempts, credential.LockedUntil)
	}
}

func TestRecoverResetsPINAndSessions(t *testing.T) {
	st, account := newFixtures(t)
	svc := New(st, fakeConnectionCreator{})
	setup, err := svc.SetupPIN(SetupPINInput{AccountID: account.ID, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := svc.Login(LoginInput{Username: account.Username, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.Recover(RecoveryInput{Username: account.Username, RecoveryCode: setup.RecoveryCode, NewPIN: "abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RecoveryCode == "" || recovered.RecoveryCode == setup.RecoveryCode {
		t.Fatal("expected rotated recovery code")
	}
	if _, _, err := svc.AuthenticateSession(login.SessionToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected old session to be revoked, got %v", err)
	}
	if _, err := svc.Login(LoginInput{Username: account.Username, PIN: "abcdef"}); err != nil {
		t.Fatal(err)
	}
}

func TestRotateRecoveryCodeInvalidatesPreviousCode(t *testing.T) {
	st, account := newFixtures(t)
	svc := New(st, fakeConnectionCreator{})
	setup, err := svc.SetupPIN(SetupPINInput{AccountID: account.ID, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := svc.Login(LoginInput{Username: account.Username, PIN: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := svc.RotateRecoveryCode(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated.SessionsRevoked {
		t.Fatal("expected sessions to be revoked")
	}
	if rotated.RecoveryMethod != RecoveryMethodLocalCode {
		t.Fatalf("unexpected recovery method %q", rotated.RecoveryMethod)
	}
	if rotated.RecoveryCode == "" || rotated.RecoveryCode == setup.RecoveryCode {
		t.Fatal("expected a new local recovery code")
	}
	if _, _, err := svc.AuthenticateSession(login.SessionToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected old session to be revoked, got %v", err)
	}
	if _, err := svc.Recover(RecoveryInput{Username: account.Username, RecoveryCode: setup.RecoveryCode, NewPIN: "abcdef"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected old recovery code to fail, got %v", err)
	}
	if _, err := svc.Recover(RecoveryInput{Username: account.Username, RecoveryCode: rotated.RecoveryCode, NewPIN: "abcdef"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(LoginInput{Username: account.Username, PIN: "abcdef"}); err != nil {
		t.Fatal(err)
	}
}

func TestEffectivePolicyAndClientLimit(t *testing.T) {
	st, account := newFixtures(t)
	node, err := st.CreateNode(model.Node{Name: "hy", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := st.CreatePolicyTag(model.PolicyTag{
		Slug:           "family",
		Name:           "Family",
		AllowedNodeIDs: []string{node.ID},
		ClientLimit:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignPolicyTag(model.AccountPolicyTag{AccountID: account.ID, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}
	svc := New(st, fakeConnectionCreator{})
	policy := svc.EffectivePolicy(account.ID)
	if policy.ClientLimit != 1 || len(policy.AllowedNodeIDs) != 1 || policy.AllowedNodeIDs[0] != node.ID {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if _, err := svc.CreateClient(context.Background(), CreateClientInput{AccountID: account.ID, ClientSlug: "phone", ClientName: "Phone"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateClient(context.Background(), CreateClientInput{AccountID: account.ID, ClientSlug: "mac", ClientName: "Mac"})
	if !errors.Is(err, ErrClientLimitReached) {
		t.Fatalf("expected client limit, got %v", err)
	}
}

func TestDeleteClientMarksClientDeleted(t *testing.T) {
	st, account := newFixtures(t)
	node, err := st.CreateNode(model.Node{Name: "hy", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := st.CreatePolicyTag(model.PolicyTag{
		Slug:           "family",
		Name:           "Family",
		AllowedNodeIDs: []string{node.ID},
		ClientLimit:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignPolicyTag(model.AccountPolicyTag{AccountID: account.ID, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}
	svc := New(st, fakeConnectionCreator{})
	created, err := svc.CreateClient(context.Background(), CreateClientInput{AccountID: account.ID, ClientSlug: "phone", ClientName: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := svc.DeleteClient(context.Background(), DeleteClientInput{AccountID: account.ID, ClientID: created.Client.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Partial {
		t.Fatal("did not expect partial delete")
	}
	if deleted.Client.Status != model.StatusDeleted {
		t.Fatalf("expected deleted client, got %+v", deleted.Client)
	}
	if count, err := st.CountActiveClientsForAccount(account.ID); err != nil || count != 0 {
		t.Fatalf("expected no active clients, count=%d err=%v", count, err)
	}
}

func newFixtures(t *testing.T) (*store.SQLStore, model.Account) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	return st, account
}
