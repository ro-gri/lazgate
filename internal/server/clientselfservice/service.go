package clientselfservice

import (
	"context"
	"strings"

	"laz/internal/server/connections"
	"laz/internal/server/model"
)

type Service struct {
	store   Store
	manager ConnectionManager
}

type Store interface {
	GetAccount(id string) (model.Account, error)
	GetConnection(id string) (model.Connection, error)
	ListConnections() []model.Connection
	ListPolicyTags() []model.PolicyTag
	ListAccountPolicyTags(accountID string) []model.AccountPolicyTag
	CountActiveConnectionsForAccount(accountID string) (int, error)
	ListNodes() []model.Node
}

type ConnectionManager interface {
	CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, name string) (connections.ConnectionResult, error)
	DeleteConnection(ctx context.Context, connection model.Connection, node model.Node) (model.Connection, error)
}

func New(st Store, manager ConnectionManager) *Service { return &Service{store: st, manager: manager} }

func (s *Service) EffectivePolicy(accountID string) model.EffectiveConnectionPolicy {
	tagByID := map[string]model.PolicyTag{}
	for _, tag := range s.store.ListPolicyTags() {
		if tag.Status == model.StatusActive {
			tagByID[tag.ID] = tag
		}
	}
	allowed := map[string]bool{}
	limit, found := 0, false
	for _, assignment := range s.store.ListAccountPolicyTags(accountID) {
		tag, ok := tagByID[assignment.TagID]
		if !ok {
			continue
		}
		found = true
		for _, nodeID := range tag.AllowedNodeIDs {
			if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
				allowed[nodeID] = true
			}
		}
		if tag.ConnectionLimit == model.ConnectionLimitUnlimited {
			limit = model.ConnectionLimitUnlimited
		} else if limit != model.ConnectionLimitUnlimited && tag.ConnectionLimit > limit {
			limit = tag.ConnectionLimit
		}
	}
	if !found {
		return model.EffectiveConnectionPolicy{}
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	return model.EffectiveConnectionPolicy{AllowedNodeIDs: ids, ConnectionLimit: limit}
}

type CreateConnectionInput struct {
	AccountID      string
	ConnectionName string
	NodeIDs        []string
}

type CreateConnectionResult struct {
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

func (s *Service) CreateConnections(ctx context.Context, input CreateConnectionInput) (CreateConnectionResult, error) {
	account, err := s.store.GetAccount(input.AccountID)
	if err != nil {
		return CreateConnectionResult{}, err
	}
	if account.Status != model.AccountStatusActive {
		return CreateConnectionResult{}, ErrForbidden
	}
	name := strings.TrimSpace(input.ConnectionName)
	if name == "" {
		return CreateConnectionResult{}, ValidationError("connection_name is required")
	}
	policy := s.EffectivePolicy(account.ID)
	if policy.ConnectionLimit == 0 {
		return CreateConnectionResult{}, ErrForbidden
	}
	nodes, err := s.allowedNodes(policy, input.NodeIDs)
	if err != nil {
		return CreateConnectionResult{}, err
	}
	if err = s.checkConnectionLimit(account.ID, policy.ConnectionLimit, len(nodes)); err != nil {
		return CreateConnectionResult{}, err
	}
	result := CreateConnectionResult{Results: make([]ConnectionResult, 0, len(nodes))}
	for _, node := range nodes {
		item := ConnectionResult{Node: node, Status: "created"}
		created, createErr := s.manager.CreateEnrollmentConnection(ctx, node, account, name)
		if createErr != nil {
			item.Status, item.Err, result.Partial = "error", createErr, true
		} else {
			item.Connection, item.ConfigCount = created.Connection, len(created.Configs)
		}
		result.Results = append(result.Results, item)
	}
	return result, nil
}

func (s *Service) DeleteConnection(ctx context.Context, accountID, connectionID string) (ConnectionResult, error) {
	account, err := s.store.GetAccount(accountID)
	if err != nil {
		return ConnectionResult{}, err
	}
	if account.Status != model.AccountStatusActive {
		return ConnectionResult{}, ErrForbidden
	}
	connection, err := s.store.GetConnection(strings.TrimSpace(connectionID))
	if err != nil {
		return ConnectionResult{}, err
	}
	if connection.AccountID != account.ID || connection.DesiredStatus == model.ConnectionStatusDeleted {
		return ConnectionResult{}, ErrForbidden
	}
	nodes := map[string]model.Node{}
	for _, node := range s.store.ListNodes() {
		nodes[node.ID] = node
	}
	node, ok := nodes[connection.NodeID]
	if !ok {
		return ConnectionResult{}, ValidationError("node not found")
	}
	updated, err := s.manager.DeleteConnection(ctx, connection, node)
	if err != nil {
		return ConnectionResult{}, err
	}
	return ConnectionResult{Node: node, Connection: updated, Status: "deleted"}, nil
}

func (s *Service) checkConnectionLimit(accountID string, limit, requested int) error {
	if limit == model.ConnectionLimitUnlimited {
		return nil
	}
	active, err := s.store.CountActiveConnectionsForAccount(accountID)
	if err != nil {
		return err
	}
	if active+requested > limit {
		return ErrConnectionLimitReached
	}
	return nil
}

func (s *Service) allowedNodes(policy model.EffectiveConnectionPolicy, requested []string) ([]model.Node, error) {
	allowed := map[string]bool{}
	for _, id := range policy.AllowedNodeIDs {
		allowed[id] = true
	}
	wanted := map[string]bool{}
	for _, id := range requested {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return nil, ValidationError("node_ids are required")
	}
	var out []model.Node
	for _, node := range s.store.ListNodes() {
		if node.Status == model.StatusActive && allowed[node.ID] && wanted[node.ID] && connections.EnrollmentProtocolForNode(node) != "" {
			out = append(out, node)
		}
	}
	if len(out) != len(wanted) {
		return nil, ErrForbidden
	}
	return out, nil
}
