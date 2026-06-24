package connections

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"laz/internal/integrations/amnezia"
	"laz/internal/integrations/blitz"
	"laz/internal/model"
	"laz/internal/services/connections/remote"
)

var ErrUnsupportedNodeType = errors.New("unsupported node type")

type Service struct {
	store       Store
	providerFor func(model.Node) (remote.Provider, error)
	newPassword func() (string, error)
}

type Store interface {
	GetClientForAccount(accountID, clientID string) (model.Client, error)
	CreateConnection(model.Connection) (model.Connection, error)
	CreateIssuedConfig(model.IssuedConfig) (model.IssuedConfig, error)
	UpdateConnectionStatus(id string, status model.Status, lastErr string) (model.Connection, error)
	RevokeConfigsForConnection(connectionID string) error
	ListNodes() []model.Node
}

func New(st Store, newPassword func() (string, error)) *Service {
	return &Service{
		store:       st,
		providerFor: defaultProviderFor,
		newPassword: newPassword,
	}
}

func defaultProviderFor(node model.Node) (remote.Provider, error) {
	switch node.Type {
	case model.NodeTypeAmneziaAPI:
		return amnezia.New(node.BaseURL, node.APIKey), nil
	case model.NodeTypeBlitzHysteria:
		return blitz.New(node), nil
	default:
		return nil, ErrUnsupportedNodeType
	}
}

func (s *Service) GetClient(accountID, clientID string) (model.Client, error) {
	return s.store.GetClientForAccount(accountID, clientID)
}

type ConnectionInput struct {
	AccountID      string
	ClientID       string
	Node           model.Node
	Protocol       model.Protocol
	RemoteName     string
	ConfigName     string
	Password       string
	TrafficLimitGB int
	ExpirationDays int
	Unlimited      bool
}

type ConnectionResult struct {
	Connection model.Connection
	Configs    []model.IssuedConfig
}

func (s *Service) CreateConnection(ctx context.Context, input ConnectionInput) (ConnectionResult, error) {
	if input.Protocol != EnrollmentProtocolForNode(input.Node) {
		return ConnectionResult{}, errors.New("unsupported protocol/node type combination")
	}
	return s.provisionRemote(ctx, input)
}

func (s *Service) CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, client model.Client, trafficLimitGB, expirationDays int) (ConnectionResult, error) {
	protocol := EnrollmentProtocolForNode(node)
	if protocol == "" {
		return ConnectionResult{}, fmt.Errorf("unsupported node type %s", node.Type)
	}
	return s.CreateConnection(ctx, ConnectionInput{
		AccountID:      account.ID,
		ClientID:       client.ID,
		Node:           node,
		Protocol:       protocol,
		RemoteName:     RemoteNameFor(account.Username, client.Slug, account.ID),
		TrafficLimitGB: trafficLimitGB,
		ExpirationDays: expirationDays,
	})
}

func (s *Service) provisionRemote(ctx context.Context, input ConnectionInput) (ConnectionResult, error) {
	provider, err := s.providerFor(input.Node)
	if err != nil {
		return ConnectionResult{}, err
	}
	configName := input.ConfigName
	if configName == "" {
		configName = defaultConfigName(input.Node, input.Protocol)
	}
	unlimited := input.Unlimited
	if input.TrafficLimitGB == 0 && input.ExpirationDays == 0 {
		unlimited = true
	}
	password := input.Password
	if password == "" {
		var err error
		password, err = s.newPassword()
		if err != nil {
			return ConnectionResult{}, err
		}
	}
	created, err := provider.CreateConnection(ctx, remote.CreateInput{
		Name:           input.RemoteName,
		ConfigName:     configName,
		Password:       password,
		TrafficLimitGB: input.TrafficLimitGB,
		ExpirationDays: input.ExpirationDays,
		Unlimited:      unlimited,
		IncludeIPv6:    input.Node.UseIPv6,
		Note:           "managed-by:laz",
	})
	if err != nil {
		return ConnectionResult{}, err
	}

	connection, err := s.store.CreateConnection(model.Connection{
		AccountID:  input.AccountID,
		ClientID:   input.ClientID,
		NodeID:     input.Node.ID,
		Protocol:   input.Protocol,
		RemoteID:   created.Ref.ID,
		RemoteName: created.Ref.Name,
	})
	if err != nil {
		_ = provider.DeleteConnection(ctx, created.Ref)
		return ConnectionResult{}, err
	}

	configs := []model.IssuedConfig{}
	for _, cfg := range created.Configs {
		config, err := s.store.CreateIssuedConfig(model.IssuedConfig{
			ConnectionID: connection.ID,
			Kind:         cfg.Kind,
			Slug:         cfg.Slug,
			Name:         cfg.Name,
			Client:       cfg.Client,
			ContentType:  cfg.ContentType,
			Config:       cfg.Value,
		})
		if err != nil {
			_ = provider.DeleteConnection(ctx, created.Ref)
			return ConnectionResult{}, err
		}
		configs = append(configs, config)
	}

	return ConnectionResult{Connection: connection, Configs: configs}, nil
}

func (s *Service) SetConnectionStatus(ctx context.Context, connection model.Connection, node model.Node, localStatus model.Status, remoteStatus string) (model.Connection, error) {
	provider, err := s.providerFor(node)
	if err == nil {
		err = provider.SetConnectionStatus(ctx, remote.Ref{ID: connection.RemoteID, Name: connection.RemoteName}, localStatus)
	}
	if err != nil {
		_, _ = s.store.UpdateConnectionStatus(connection.ID, model.StatusError, err.Error())
		return model.Connection{}, err
	}
	return s.store.UpdateConnectionStatus(connection.ID, localStatus, "")
}

func (s *Service) DeleteConnection(ctx context.Context, connection model.Connection, node model.Node) (model.Connection, error) {
	provider, err := s.providerFor(node)
	if err == nil {
		err = provider.DeleteConnection(ctx, remote.Ref{ID: connection.RemoteID, Name: connection.RemoteName})
	}
	if err != nil {
		_, _ = s.store.UpdateConnectionStatus(connection.ID, model.StatusError, err.Error())
		return model.Connection{}, err
	}
	if err := s.store.RevokeConfigsForConnection(connection.ID); err != nil {
		return model.Connection{}, err
	}
	return s.store.UpdateConnectionStatus(connection.ID, model.StatusDeleted, "")
}

func (s *Service) SelectEnrollmentNodes(mode string, nodeIDs []string) ([]model.Node, error) {
	byID := map[string]model.Node{}
	for _, node := range s.store.ListNodes() {
		byID[node.ID] = node
	}
	if strings.EqualFold(strings.TrimSpace(mode), "all") {
		nodes := []model.Node{}
		for _, node := range s.store.ListNodes() {
			if node.Status == model.StatusActive && EnrollmentProtocolForNode(node) != "" {
				nodes = append(nodes, node)
			}
		}
		if len(nodes) == 0 {
			return nil, errors.New("no active supported nodes found")
		}
		return nodes, nil
	}
	if len(nodeIDs) == 0 {
		return nil, errors.New("node_ids are required unless nodes is all")
	}
	nodes := []model.Node{}
	seen := map[string]bool{}
	for _, id := range nodeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		node, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("node %s not found", id)
		}
		if node.Status != model.StatusActive {
			return nil, fmt.Errorf("node %s is not active", id)
		}
		if EnrollmentProtocolForNode(node) == "" {
			return nil, fmt.Errorf("node %s has unsupported type %s", id, node.Type)
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("no nodes selected")
	}
	return nodes, nil
}

func (s *Service) ListRemoteConnections(ctx context.Context, node model.Node) (remote.ConnectionList, error) {
	provider, err := s.providerFor(node)
	if err != nil {
		return remote.ConnectionList{}, err
	}
	return provider.ListConnections(ctx)
}

func EnrollmentProtocolForNode(node model.Node) model.Protocol {
	switch node.Type {
	case model.NodeTypeAmneziaAPI:
		return model.ProtocolAmneziaWG
	case model.NodeTypeBlitzHysteria:
		return model.ProtocolHysteria2
	default:
		return ""
	}
}

func RemoteNameFor(username, clientSlug, accountID string) string {
	return safeIdentifier(username + "_" + clientSlug + "_" + shortID(accountID))
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "usr_")
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "account"
	}
	return id
}

func defaultConfigName(node model.Node, protocol model.Protocol) string {
	switch protocol {
	case model.ProtocolAmneziaWG:
		return node.Name + " AmneziaWG"
	case model.ProtocolHysteria2:
		return node.Name + " Hysteria2"
	default:
		return node.Name
	}
}

func safeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "account"
	}
	return out
}
