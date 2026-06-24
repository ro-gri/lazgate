package clientauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	commontokens "laz/internal/common/tokens"
	"laz/internal/model"
	"laz/internal/services/connections"
	"laz/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultSessionTTL = 30 * 24 * time.Hour
	maxFailedAttempts = 5
	lockoutDuration   = 15 * time.Minute
)

type Service struct {
	store             Store
	connectionCreator ConnectionManager
	now               func() time.Time
}

type Store interface {
	GetAccount(id string) (model.Account, error)
	ListAccounts() []model.Account
	CreateClient(model.Client) (model.Client, error)
	GetClientForAccount(accountID, clientID string) (model.Client, error)
	UpdateClientStatus(id string, status model.Status) (model.Client, error)
	ListConnections() []model.Connection
	GetClientCredential(accountID string) (model.ClientCredential, error)
	UpsertClientCredential(model.ClientCredential) (model.ClientCredential, error)
	UpdateClientCredentialAuthState(accountID string, failedAttempts int, lockedUntil time.Time) (model.ClientCredential, error)
	CreateClientSession(model.ClientSession) (model.ClientSession, error)
	GetClientSessionByHash(hash string) (model.ClientSession, error)
	TouchClientSession(id string) error
	RevokeClientSession(id string) error
	RevokeClientSessionsForAccount(accountID string) error
	ListPolicyTags() []model.PolicyTag
	ListAccountPolicyTags(accountID string) []model.AccountPolicyTag
	CountActiveClientsForAccount(accountID string) (int, error)
	ListNodes() []model.Node
}

type ConnectionManager interface {
	CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, client model.Client, trafficLimitGB, expirationDays int) (connections.ConnectionResult, error)
	DeleteConnection(ctx context.Context, connection model.Connection, node model.Node) (model.Connection, error)
}

func New(st Store, connectionCreator ConnectionManager) *Service {
	return &Service{store: st, connectionCreator: connectionCreator, now: func() time.Time { return time.Now().UTC() }}
}

type SetupPINInput struct {
	AccountID string
	PIN       string
}

type SetupPINResult struct {
	Credential      model.ClientCredential
	RecoveryCode    string
	RecoveryMethod  RecoveryMethod
	SessionsRevoked bool
}

type RecoveryMethod string

const RecoveryMethodLocalCode RecoveryMethod = "local_code"

func (s *Service) SetupPIN(input SetupPINInput) (SetupPINResult, error) {
	if strings.TrimSpace(input.AccountID) == "" {
		return SetupPINResult{}, ValidationError("account_id is required")
	}
	if err := validatePIN(input.PIN); err != nil {
		return SetupPINResult{}, err
	}
	account, err := s.store.GetAccount(input.AccountID)
	if err != nil {
		return SetupPINResult{}, err
	}
	if account.Status != model.StatusActive {
		return SetupPINResult{}, ErrForbidden
	}
	return s.upsertPINAndLocalRecoveryCode(input.AccountID, input.PIN)
}

func (s *Service) RotateRecoveryCode(accountID string) (SetupPINResult, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return SetupPINResult{}, ValidationError("account_id is required")
	}
	account, err := s.store.GetAccount(accountID)
	if err != nil {
		return SetupPINResult{}, err
	}
	if account.Status != model.StatusActive {
		return SetupPINResult{}, ErrForbidden
	}
	credential, err := s.store.GetClientCredential(account.ID)
	if err != nil {
		return SetupPINResult{}, err
	}
	recoveryCode, err := newRecoveryCode()
	if err != nil {
		return SetupPINResult{}, err
	}
	recoveryHash, err := hashSecret(recoveryCode)
	if err != nil {
		return SetupPINResult{}, err
	}
	credential.RecoveryCodeHash = recoveryHash
	credential.FailedAttempts = 0
	credential.LockedUntil = time.Time{}
	credential, err = s.store.UpsertClientCredential(credential)
	if err != nil {
		return SetupPINResult{}, err
	}
	if err := s.store.RevokeClientSessionsForAccount(account.ID); err != nil {
		return SetupPINResult{}, err
	}
	return SetupPINResult{Credential: credential, RecoveryCode: recoveryCode, RecoveryMethod: RecoveryMethodLocalCode, SessionsRevoked: true}, nil
}

func (s *Service) upsertPINAndLocalRecoveryCode(accountID, pin string) (SetupPINResult, error) {
	recoveryCode, err := newRecoveryCode()
	if err != nil {
		return SetupPINResult{}, err
	}
	pinHash, err := hashSecret(pin)
	if err != nil {
		return SetupPINResult{}, err
	}
	recoveryHash, err := hashSecret(recoveryCode)
	if err != nil {
		return SetupPINResult{}, err
	}
	credential, err := s.store.UpsertClientCredential(model.ClientCredential{
		AccountID:        accountID,
		PINHash:          pinHash,
		RecoveryCodeHash: recoveryHash,
	})
	if err != nil {
		return SetupPINResult{}, err
	}
	return SetupPINResult{Credential: credential, RecoveryCode: recoveryCode, RecoveryMethod: RecoveryMethodLocalCode}, nil
}

type LoginInput struct {
	AccountID string
	Username  string
	PIN       string
}

type LoginResult struct {
	SessionToken string
	Session      model.ClientSession
	Account      model.Account
}

func (s *Service) Login(input LoginInput) (LoginResult, error) {
	account, err := s.accountForAuth(input.AccountID, input.Username)
	if err != nil {
		return LoginResult{}, err
	}
	if account.Status != model.StatusActive {
		return LoginResult{}, ErrForbidden
	}
	credential, err := s.store.GetClientCredential(account.ID)
	if err != nil {
		return LoginResult{}, err
	}
	credential, err = s.resetExpiredLockout(credential)
	if err != nil {
		return LoginResult{}, err
	}
	if s.locked(credential) {
		return LoginResult{}, ErrLocked
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.PINHash), []byte(input.PIN)) != nil {
		_, _ = s.registerFailedAttempt(credential)
		return LoginResult{}, ErrInvalidCredentials
	}
	_, _ = s.store.UpdateClientCredentialAuthState(account.ID, 0, time.Time{})
	return s.createSession(account)
}

type RecoveryInput struct {
	AccountID    string
	Username     string
	RecoveryCode string
	NewPIN       string
}

func (s *Service) Recover(input RecoveryInput) (SetupPINResult, error) {
	if err := validatePIN(input.NewPIN); err != nil {
		return SetupPINResult{}, err
	}
	account, err := s.accountForAuth(input.AccountID, input.Username)
	if err != nil {
		return SetupPINResult{}, err
	}
	if account.Status != model.StatusActive {
		return SetupPINResult{}, ErrForbidden
	}
	credential, err := s.store.GetClientCredential(account.ID)
	if err != nil {
		return SetupPINResult{}, err
	}
	credential, err = s.resetExpiredLockout(credential)
	if err != nil {
		return SetupPINResult{}, err
	}
	if s.locked(credential) {
		return SetupPINResult{}, ErrLocked
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.RecoveryCodeHash), []byte(input.RecoveryCode)) != nil {
		_, _ = s.registerFailedAttempt(credential)
		return SetupPINResult{}, ErrInvalidCredentials
	}
	_ = s.store.RevokeClientSessionsForAccount(account.ID)
	result, err := s.upsertPINAndLocalRecoveryCode(account.ID, input.NewPIN)
	if err != nil {
		return SetupPINResult{}, err
	}
	result.SessionsRevoked = true
	return result, nil
}

func (s *Service) AuthenticateSession(raw string) (model.ClientSession, model.Account, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return model.ClientSession{}, model.Account{}, ErrInvalidCredentials
	}
	session, err := s.store.GetClientSessionByHash(commontokens.Hash(raw))
	if err != nil {
		return model.ClientSession{}, model.Account{}, err
	}
	if session.Status != model.StatusActive {
		return model.ClientSession{}, model.Account{}, ErrForbidden
	}
	if !session.ExpiresAt.IsZero() && s.now().After(session.ExpiresAt) {
		_ = s.store.RevokeClientSession(session.ID)
		return model.ClientSession{}, model.Account{}, ErrForbidden
	}
	account, err := s.store.GetAccount(session.AccountID)
	if err != nil {
		return model.ClientSession{}, model.Account{}, err
	}
	if account.Status != model.StatusActive {
		return model.ClientSession{}, model.Account{}, ErrForbidden
	}
	_ = s.store.TouchClientSession(session.ID)
	return session, account, nil
}

func (s *Service) Logout(raw string) error {
	session, _, err := s.AuthenticateSession(raw)
	if err != nil {
		return err
	}
	return s.store.RevokeClientSession(session.ID)
}

func (s *Service) EffectivePolicy(accountID string) model.EffectiveClientPolicy {
	tags := s.store.ListPolicyTags()
	tagByID := map[string]model.PolicyTag{}
	for _, tag := range tags {
		if tag.Status == model.StatusActive {
			tagByID[tag.ID] = tag
		}
	}
	allowed := map[string]bool{}
	limit := 0
	hasPolicy := false
	for _, userTag := range s.store.ListAccountPolicyTags(accountID) {
		tag, ok := tagByID[userTag.TagID]
		if !ok {
			continue
		}
		hasPolicy = true
		for _, nodeID := range tag.AllowedNodeIDs {
			if strings.TrimSpace(nodeID) != "" {
				allowed[nodeID] = true
			}
		}
		if tag.ClientLimit == model.ClientLimitUnlimited {
			limit = model.ClientLimitUnlimited
		} else if limit != model.ClientLimitUnlimited && tag.ClientLimit > limit {
			limit = tag.ClientLimit
		}
	}
	if !hasPolicy {
		return model.EffectiveClientPolicy{ClientLimit: 0}
	}
	nodeIDs := make([]string, 0, len(allowed))
	for nodeID := range allowed {
		nodeIDs = append(nodeIDs, nodeID)
	}
	return model.EffectiveClientPolicy{AllowedNodeIDs: nodeIDs, ClientLimit: limit}
}

type CreateClientInput struct {
	AccountID  string
	ClientSlug string
	ClientName string
	NodeIDs    []string
}

type CreateClientResult struct {
	Client  model.Client
	Results []ConnectionResult
	Partial bool
}

type ConnectionResult struct {
	Node        model.Node
	Connection  model.Connection
	ConfigCount int
	Status      string
	Err         error
}

func (s *Service) CreateClient(ctx context.Context, input CreateClientInput) (CreateClientResult, error) {
	account, err := s.store.GetAccount(input.AccountID)
	if err != nil {
		return CreateClientResult{}, err
	}
	if account.Status != model.StatusActive {
		return CreateClientResult{}, ErrForbidden
	}
	if strings.TrimSpace(input.ClientSlug) == "" || strings.TrimSpace(input.ClientName) == "" {
		return CreateClientResult{}, ValidationError("client_slug and client_name are required")
	}
	policy := s.EffectivePolicy(account.ID)
	if policy.ClientLimit == 0 {
		return CreateClientResult{}, ErrForbidden
	}
	if err := s.checkClientLimit(account.ID, policy.ClientLimit); err != nil {
		return CreateClientResult{}, err
	}
	allowedNodes, err := s.allowedNodes(policy, input.NodeIDs)
	if err != nil {
		return CreateClientResult{}, err
	}
	if len(allowedNodes) == 0 {
		return CreateClientResult{}, ValidationError("no allowed nodes selected")
	}
	client, err := s.store.CreateClient(model.Client{
		AccountID: account.ID,
		Slug:      strings.TrimSpace(input.ClientSlug),
		Name:      strings.TrimSpace(input.ClientName),
	})
	if err != nil {
		return CreateClientResult{}, err
	}
	results := make([]ConnectionResult, 0, len(allowedNodes))
	for _, node := range allowedNodes {
		result := ConnectionResult{Node: node, Status: "created"}
		created, err := s.connectionCreator.CreateEnrollmentConnection(ctx, node, account, client, 0, 0)
		if err != nil {
			result.Status = "error"
			result.Err = err
			results = append(results, result)
			continue
		}
		result.Connection = created.Connection
		result.ConfigCount = len(created.Configs)
		results = append(results, result)
	}
	partial := false
	for _, result := range results {
		if result.Err != nil {
			partial = true
			break
		}
	}
	return CreateClientResult{Client: client, Results: results, Partial: partial}, nil
}

type DeleteClientInput struct {
	AccountID string
	ClientID  string
}

type DeleteClientResult struct {
	Client  model.Client
	Results []ConnectionResult
	Partial bool
}

func (s *Service) DeleteClient(ctx context.Context, input DeleteClientInput) (DeleteClientResult, error) {
	account, err := s.store.GetAccount(input.AccountID)
	if err != nil {
		return DeleteClientResult{}, err
	}
	if account.Status != model.StatusActive {
		return DeleteClientResult{}, ErrForbidden
	}
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		return DeleteClientResult{}, ValidationError("client_id is required")
	}
	client, err := s.store.GetClientForAccount(account.ID, clientID)
	if err != nil {
		return DeleteClientResult{}, err
	}
	if client.Status != model.StatusActive {
		return DeleteClientResult{}, ErrForbidden
	}
	nodesByID := map[string]model.Node{}
	for _, node := range s.store.ListNodes() {
		nodesByID[node.ID] = node
	}
	connections := s.clientConnections(account.ID, client.ID)
	results := make([]ConnectionResult, 0, len(connections))
	partial := false
	for _, connection := range connections {
		node, ok := nodesByID[connection.NodeID]
		if !ok {
			result := ConnectionResult{Connection: connection, Status: "error", Err: ValidationError("node not found")}
			results = append(results, result)
			partial = true
			continue
		}
		result := ConnectionResult{Node: node, Connection: connection, Status: "deleted"}
		deleted, err := s.connectionCreator.DeleteConnection(ctx, connection, node)
		if err != nil {
			result.Status = "error"
			result.Err = err
			partial = true
		} else {
			result.Connection = deleted
		}
		results = append(results, result)
	}
	if partial {
		return DeleteClientResult{Client: client, Results: results, Partial: true}, nil
	}
	deletedClient, err := s.store.UpdateClientStatus(client.ID, model.StatusDeleted)
	if err != nil {
		return DeleteClientResult{}, err
	}
	return DeleteClientResult{Client: deletedClient, Results: results}, nil
}

func (s *Service) clientConnections(accountID, clientID string) []model.Connection {
	out := []model.Connection{}
	for _, connection := range s.store.ListConnections() {
		if connection.AccountID != accountID || connection.ClientID != clientID || connection.Status == model.StatusDeleted {
			continue
		}
		out = append(out, connection)
	}
	return out
}

func (s *Service) createSession(account model.Account) (LoginResult, error) {
	raw, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	session, err := s.store.CreateClientSession(model.ClientSession{
		AccountID: account.ID,
		TokenHash: commontokens.Hash(raw),
		ExpiresAt: s.now().Add(defaultSessionTTL),
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{SessionToken: raw, Session: session, Account: account}, nil
}

func (s *Service) registerFailedAttempt(credential model.ClientCredential) (model.ClientCredential, error) {
	attempts := credential.FailedAttempts + 1
	lockedUntil := time.Time{}
	if attempts >= maxFailedAttempts {
		lockedUntil = s.now().Add(lockoutDuration)
	}
	return s.store.UpdateClientCredentialAuthState(credential.AccountID, attempts, lockedUntil)
}

func (s *Service) resetExpiredLockout(credential model.ClientCredential) (model.ClientCredential, error) {
	if credential.LockedUntil.IsZero() || s.now().Before(credential.LockedUntil) {
		return credential, nil
	}
	return s.store.UpdateClientCredentialAuthState(credential.AccountID, 0, time.Time{})
}

func (s *Service) locked(credential model.ClientCredential) bool {
	return !credential.LockedUntil.IsZero() && s.now().Before(credential.LockedUntil)
}

func (s *Service) userByUsername(username string) (model.Account, error) {
	username = strings.TrimSpace(username)
	for _, account := range s.store.ListAccounts() {
		if strings.EqualFold(account.Username, username) {
			return account, nil
		}
	}
	return model.Account{}, store.ErrNotFound
}

func (s *Service) accountForAuth(accountID, username string) (model.Account, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID != "" {
		return s.store.GetAccount(accountID)
	}
	return s.userByUsername(username)
}

func (s *Service) checkClientLimit(accountID string, limit int) error {
	if limit == model.ClientLimitUnlimited {
		return nil
	}
	active, err := s.store.CountActiveClientsForAccount(accountID)
	if err != nil {
		return err
	}
	if active >= limit {
		return ErrClientLimitReached
	}
	return nil
}

func (s *Service) allowedNodes(policy model.EffectiveClientPolicy, requested []string) ([]model.Node, error) {
	allowed := map[string]bool{}
	for _, nodeID := range policy.AllowedNodeIDs {
		allowed[nodeID] = true
	}
	requestedSet := map[string]bool{}
	for _, nodeID := range requested {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID != "" {
			requestedSet[nodeID] = true
		}
	}
	out := []model.Node{}
	for _, node := range s.store.ListNodes() {
		if node.Status != model.StatusActive || connections.EnrollmentProtocolForNode(node) == "" {
			continue
		}
		if !allowed[node.ID] {
			continue
		}
		if len(requestedSet) > 0 && !requestedSet[node.ID] {
			continue
		}
		out = append(out, node)
	}
	if len(requestedSet) > 0 && len(out) != len(requestedSet) {
		return nil, ErrForbidden
	}
	return out, nil
}

func validatePIN(pin string) error {
	pin = strings.TrimSpace(pin)
	if len([]rune(pin)) < 6 {
		return ValidationError("pin must be at least 6 characters")
	}
	return nil
}

func hashSecret(value string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	return string(raw), err
}

func newRecoveryCode() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	encoded := strings.ToUpper(base64.RawURLEncoding.EncodeToString(raw[:]))
	if len(encoded) > 24 {
		encoded = encoded[:24]
	}
	return encoded[:6] + "-" + encoded[6:12] + "-" + encoded[12:18] + "-" + encoded[18:24], nil
}
