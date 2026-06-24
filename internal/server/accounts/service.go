package accounts

import (
	"context"
	"strings"

	"laz/internal/server/apperrors"
	"laz/internal/server/connections"
	"laz/internal/server/model"
)

type ValidationError = apperrors.ValidationError

type ConnectionCreator interface {
	SelectEnrollmentNodes(nodeIDs []string) ([]model.Node, error)
	CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, name string) (connections.ConnectionResult, error)
}

type Store interface {
	CreateAccount(model.Account) (model.Account, error)
	GetAccount(id string) (model.Account, error)
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

type CreateAccountInput struct {
	Username    string
	DisplayName string
}

func (input CreateAccountInput) Validate() error {
	if strings.TrimSpace(input.Username) == "" {
		return ValidationError("username is required")
	}
	return nil
}

func (s *Service) CreateAccount(input CreateAccountInput) (model.Account, error) {
	return s.store.CreateAccount(model.Account{Username: strings.TrimSpace(input.Username), DisplayName: strings.TrimSpace(input.DisplayName)})
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

func (s *Service) CreateIssuedConfig(input CreateIssuedConfigInput) (model.IssuedConfig, error) {
	return s.store.CreateIssuedConfig(model.IssuedConfig{ConnectionID: input.ConnectionID, Kind: input.Kind, Slug: input.Slug, Name: input.Name, Client: input.Client, ContentType: input.ContentType, Config: input.Config})
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

func (s *Service) CreateConfigProfile(input CreateConfigProfileInput) (model.ConfigProfile, error) {
	return s.store.CreateConfigProfile(model.ConfigProfile{Protocol: input.Protocol, Kind: input.Kind, Slug: input.Slug, Name: input.Name, Client: input.Client, ContentType: input.ContentType, ConfigTemplate: input.ConfigTemplate})
}

type EnrollmentInput struct {
	Username       string
	DisplayName    string
	ConnectionName string
	NodeIDs        []string
}

func (input EnrollmentInput) Validate() error {
	if strings.TrimSpace(input.Username) == "" {
		return ValidationError("username is required")
	}
	if strings.TrimSpace(input.ConnectionName) == "" {
		return ValidationError("connection_name is required")
	}
	return nil
}

type EnrollmentResult struct {
	Account    model.Account
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
	nodes, err := s.connectionCreator.SelectEnrollmentNodes(input.NodeIDs)
	if err != nil {
		return EnrollmentResult{}, err
	}
	account, err := s.CreateAccount(CreateAccountInput{Username: input.Username, DisplayName: input.DisplayName})
	if err != nil {
		return EnrollmentResult{}, err
	}
	result := EnrollmentResult{Account: account, NodeCount: len(nodes)}
	for _, node := range nodes {
		item := EnrollmentNodeResult{Node: node}
		created, createErr := s.connectionCreator.CreateEnrollmentConnection(ctx, node, account, strings.TrimSpace(input.ConnectionName))
		if createErr != nil {
			item.Status, item.Err = "error", createErr
		} else {
			result.Successes++
			item.Status, item.Connection, item.ConfigCount = "created", created.Connection, len(created.Configs)
		}
		result.Results = append(result.Results, item)
	}
	result.Partial = result.Successes != len(nodes)
	result.Summary, result.SummaryErr = s.store.Summary(account.ID)
	return result, nil
}
