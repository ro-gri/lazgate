package accounts

import (
	"context"
	"strings"

	"laz/internal/common/apperrors"
	"laz/internal/model"
	"laz/internal/services/connections"
)

type ValidationError = apperrors.ValidationError

type ConnectionCreator interface {
	SelectEnrollmentNodes(mode string, nodeIDs []string) ([]model.Node, error)
	CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, client model.Client, trafficLimitGB, expirationDays int) (connections.ConnectionResult, error)
}

type Store interface {
	CreateAccount(model.Account) (model.Account, error)
	GetAccount(id string) (model.Account, error)
	CreateClient(model.Client) (model.Client, error)
	CreateConnection(model.Connection) (model.Connection, error)
	CreateIssuedConfig(model.IssuedConfig) (model.IssuedConfig, error)
	CreateConfigProfile(model.ConfigProfile) (model.ConfigProfile, error)
	Summary(accountID string) (model.AccountSummary, error)
}

type Service struct {
	store             Store
	connectionCreator ConnectionCreator
}

func New(st Store, connectionCreator ConnectionCreator) *Service {
	return &Service{store: st, connectionCreator: connectionCreator}
}

func (s *Service) CreateAccount(input CreateAccountInput) (model.Account, error) {
	return s.store.CreateAccount(model.Account{
		Username:    input.Username,
		DisplayName: input.DisplayName,
		Note:        input.Note,
	})
}

type CreateAccountInput struct {
	Username    string
	DisplayName string
	Note        string
}

func (input CreateAccountInput) Validate() error {
	if strings.TrimSpace(input.Username) == "" {
		return ValidationError("username is required")
	}
	return nil
}

func (s *Service) CreateClient(input CreateClientInput) (model.Client, error) {
	return s.store.CreateClient(model.Client{
		AccountID: input.AccountID,
		Slug:      input.Slug,
		Name:      input.Name,
	})
}

type CreateClientInput struct {
	AccountID string
	Slug      string
	Name      string
}

func (input CreateClientInput) Validate() error {
	if input.AccountID == "" || input.Slug == "" || input.Name == "" {
		return ValidationError("account_id, slug and name are required")
	}
	return nil
}

func (s *Service) CreateConnection(input CreateConnectionInput) (model.Connection, error) {
	return s.store.CreateConnection(model.Connection{
		AccountID:  input.AccountID,
		ClientID:   input.ClientID,
		NodeID:     input.NodeID,
		Protocol:   input.Protocol,
		RemoteID:   input.RemoteID,
		RemoteName: input.RemoteName,
	})
}

type CreateConnectionInput struct {
	AccountID  string
	ClientID   string
	NodeID     string
	Protocol   model.Protocol
	RemoteID   string
	RemoteName string
}

func (input CreateConnectionInput) Validate() error {
	if input.AccountID == "" || input.ClientID == "" || input.NodeID == "" || input.Protocol == "" {
		return ValidationError("account_id, client_id, node_id and protocol are required")
	}
	return nil
}

func (s *Service) CreateIssuedConfig(input CreateIssuedConfigInput) (model.IssuedConfig, error) {
	return s.store.CreateIssuedConfig(model.IssuedConfig{
		ConnectionID: input.ConnectionID,
		Kind:         input.Kind,
		Slug:         input.Slug,
		Name:         input.Name,
		Client:       input.Client,
		ContentType:  input.ContentType,
		Config:       input.Config,
	})
}

type CreateIssuedConfigInput struct {
	ConnectionID string
	Kind         model.ConfigKind
	Slug         string
	Name         string
	Client       string
	ContentType  string
	Config       string
}

func (input CreateIssuedConfigInput) Validate() error {
	if input.ConnectionID == "" || input.Kind == "" || input.Name == "" || input.Config == "" {
		return ValidationError("connection_id, kind, name and config are required")
	}
	return nil
}

func (s *Service) CreateConfigProfile(input CreateConfigProfileInput) (model.ConfigProfile, error) {
	return s.store.CreateConfigProfile(model.ConfigProfile{
		Protocol:       input.Protocol,
		Kind:           input.Kind,
		Slug:           input.Slug,
		Name:           input.Name,
		Client:         input.Client,
		ContentType:    input.ContentType,
		ConfigTemplate: input.ConfigTemplate,
	})
}

type CreateConfigProfileInput struct {
	Protocol       model.Protocol
	Kind           model.ConfigKind
	Slug           string
	Name           string
	Client         string
	ContentType    string
	ConfigTemplate string
}

func (input CreateConfigProfileInput) Validate() error {
	if input.Protocol == "" || input.Kind == "" || input.Slug == "" || input.Name == "" {
		return ValidationError("protocol, kind, slug and name are required")
	}
	return nil
}

type EnrollmentInput struct {
	Username       string
	DisplayName    string
	Note           string
	ClientSlug     string
	ClientName     string
	Nodes          string
	NodeIDs        []string
	TrafficLimitGB int
	ExpirationDays int
}

func (input EnrollmentInput) Validate() error {
	if strings.TrimSpace(input.Username) == "" {
		return ValidationError("username is required")
	}
	if strings.TrimSpace(input.ClientSlug) == "" || strings.TrimSpace(input.ClientName) == "" {
		return ValidationError("client.slug and client.name are required")
	}
	return nil
}

type EnrollmentResult struct {
	Account    model.Account
	Client     model.Client
	NodeCount  int
	Results    []EnrollmentNodeResult
	Successes  int
	Partial    bool
	Summary    model.AccountSummary
	SummaryErr error
}

type EnrollmentNodeResult struct {
	Node        model.Node
	Status      string
	Connection  model.Connection
	ConfigCount int
	Err         error
}

func (s *Service) Enroll(ctx context.Context, input EnrollmentInput) (EnrollmentResult, error) {
	if err := input.Validate(); err != nil {
		return EnrollmentResult{}, err
	}
	nodes, err := s.connectionCreator.SelectEnrollmentNodes(input.Nodes, input.NodeIDs)
	if err != nil {
		return EnrollmentResult{}, err
	}
	account, err := s.CreateAccount(CreateAccountInput{
		Username:    input.Username,
		DisplayName: input.DisplayName,
		Note:        input.Note,
	})
	if err != nil {
		return EnrollmentResult{}, err
	}
	client, err := s.CreateClient(CreateClientInput{
		AccountID: account.ID,
		Slug:      input.ClientSlug,
		Name:      input.ClientName,
	})
	if err != nil {
		return EnrollmentResult{}, err
	}

	result := EnrollmentResult{
		Account:   account,
		Client:    client,
		NodeCount: len(nodes),
	}
	for _, node := range nodes {
		item := EnrollmentNodeResult{Node: node}
		created, err := s.connectionCreator.CreateEnrollmentConnection(ctx, node, account, client, input.TrafficLimitGB, input.ExpirationDays)
		if err != nil {
			item.Status = "error"
			item.Err = err
			result.Results = append(result.Results, item)
			continue
		}
		result.Successes++
		item.Status = "created"
		item.Connection = created.Connection
		item.ConfigCount = len(created.Configs)
		result.Results = append(result.Results, item)
	}
	result.Partial = result.Successes != len(nodes)
	result.Summary, result.SummaryErr = s.store.Summary(account.ID)
	return result, nil
}
