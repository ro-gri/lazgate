package connections

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	transportstore "laz/internal/queue"
	"laz/internal/server/connections/remote"
	"laz/internal/server/integrations/amnezia"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/integrations/nativehy2"
	"laz/internal/server/model"
	storeutil "laz/internal/server/storage"
	"laz/internal/server/workqueue"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/language"
)

var ErrUnsupportedNodeType = errors.New("unsupported node type")

type Store interface {
	GetAccount(id string) (model.Account, error)
	CreateConnectionBundle(model.ConnectionBundle) (model.ConnectionBundle, error)
	GetConnection(id string) (model.Connection, error)
	UpdateConnectionDesiredStatus(id string, status model.ConnectionStatus) (model.Connection, error)
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedVersionMS int64) ([]model.Connection, error)
	ListConnections() []model.Connection
	ListNodes() []model.Node
	GetHysteria2NodeConfig(nodeID string) (model.Hysteria2NodeConfig, error)
	GetAmneziaWGNodeConfig(nodeID string) (model.AmneziaWGNodeConfig, error)
	ListAmneziaWGConnectionMaterials(nodeID string) []model.AmneziaWGConnectionMaterial
	GetAmneziaAPINodeConfig(nodeID string) (model.AmneziaAPINodeConfig, error)
	GetAmneziaAPIConnectionRef(connectionID string) (model.AmneziaAPIConnectionRef, error)
}

type Service struct {
	store       Store
	newPassword func() (string, error)
	transport   transportstore.Store
}

func New(st Store, newPassword func() (string, error)) *Service {
	return &Service{store: st, newPassword: newPassword, transport: transportstore.NopStore{}}
}

func (s *Service) SetTransport(v transportstore.Store) {
	if v == nil {
		v = transportstore.NopStore{}
	}
	s.transport = v
}

type ConnectionInput struct {
	AccountID string
	Node      model.Node
	Protocol  model.Protocol
	Name      string
}

type ConnectionResult struct {
	Connection model.Connection
	Configs    []model.IssuedConfig
}

func (s *Service) CreateEnrollmentConnection(ctx context.Context, node model.Node, account model.Account, name string) (ConnectionResult, error) {
	protocol := EnrollmentProtocolForNode(node)
	if protocol == "" {
		return ConnectionResult{}, fmt.Errorf("unsupported node type %s", node.Type)
	}
	return s.CreateConnection(ctx, ConnectionInput{AccountID: account.ID, Node: node, Protocol: protocol, Name: name})
}

func (s *Service) CreateConnection(ctx context.Context, input ConnectionInput) (ConnectionResult, error) {
	if input.Protocol != EnrollmentProtocolForNode(input.Node) {
		return ConnectionResult{}, errors.New("unsupported protocol/node type combination")
	}
	account, err := s.store.GetAccount(input.AccountID)
	if err != nil {
		return ConnectionResult{}, err
	}
	if account.Status != model.AccountStatusActive {
		return ConnectionResult{}, errors.New("account is not active")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ConnectionResult{}, errors.New("connection name is required")
	}
	connection := model.Connection{ID: storeutil.NewID("con"), AccountID: account.ID, NodeID: input.Node.ID, Name: name, Protocol: input.Protocol, DesiredStatus: model.ConnectionStatusActive, AppliedStatus: model.ConnectionStatusAbsent}
	switch input.Node.Type {
	case model.NodeTypeNativeHy2:
		return s.createNativeHysteria(ctx, connection, input)
	case model.NodeTypeNativeAmneziaWG:
		return s.createNativeAmneziaWG(ctx, connection, input)
	case model.NodeTypeAmneziaAPI:
		return s.createAmneziaAPI(ctx, connection, input)
	default:
		return ConnectionResult{}, ErrUnsupportedNodeType
	}
}

func (s *Service) createNativeHysteria(ctx context.Context, connection model.Connection, input ConnectionInput) (ConnectionResult, error) {
	config, err := s.store.GetHysteria2NodeConfig(connection.NodeID)
	if err != nil {
		return ConnectionResult{}, err
	}
	password, err := s.newPassword()
	if err != nil {
		return ConnectionResult{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ConnectionResult{}, fmt.Errorf("hash Hysteria password: %w", err)
	}
	title := ConfigName(input.Node, connection.Name)
	issued := nativehy2.NewIssuedConfig(config, connection.ID, password, title)
	bundle, err := s.store.CreateConnectionBundle(model.ConnectionBundle{Connection: connection, Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: password, PasswordHash: string(passwordHash)}, Configs: []model.IssuedConfig{issued}})
	if err != nil {
		return ConnectionResult{}, err
	}
	if err = s.enqueueAuthRefresh(ctx, bundle.Connection); err != nil {
		return ConnectionResult{}, err
	}
	return ConnectionResult{Connection: bundle.Connection, Configs: bundle.Configs}, nil
}

func (s *Service) createNativeAmneziaWG(ctx context.Context, connection model.Connection, input ConnectionInput) (ConnectionResult, error) {
	config, err := s.store.GetAmneziaWGNodeConfig(connection.NodeID)
	if err != nil {
		return ConnectionResult{}, err
	}
	privateKey, publicKey, err := nativeawg.GenerateKeyPair()
	if err != nil {
		return ConnectionResult{}, err
	}
	psk, err := nativeawg.GeneratePresharedKey()
	if err != nil {
		return ConnectionResult{}, err
	}
	assigned, err := nextAvailableIPv4(config, s.store.ListAmneziaWGConnectionMaterials(connection.NodeID))
	if err != nil {
		return ConnectionResult{}, err
	}
	material := model.AmneziaWGConnectionMaterial{ConnectionID: connection.ID, NodeID: connection.NodeID, ClientPublicKey: publicKey, ClientPrivateKey: privateKey, PresharedKey: psk, AssignedIPv4: assigned}
	conf, err := nativeawg.RenderClientConf(config, material)
	if err != nil {
		return ConnectionResult{}, err
	}
	title := ConfigName(input.Node, connection.Name)
	issued := nativeawg.NewIssuedAmneziaConf(connection.ID, title, conf)
	bundle, err := s.store.CreateConnectionBundle(model.ConnectionBundle{ExpectedNodeConfigVersionMS: config.ConfigVersionMS, Connection: connection, AmneziaWGMaterial: &material, Configs: []model.IssuedConfig{issued}})
	if err != nil {
		return ConnectionResult{}, err
	}
	if err = s.enqueueAuthRefresh(ctx, bundle.Connection); err != nil {
		return ConnectionResult{}, err
	}
	return ConnectionResult{Connection: bundle.Connection, Configs: bundle.Configs}, nil
}

func (s *Service) createAmneziaAPI(ctx context.Context, connection model.Connection, input ConnectionInput) (ConnectionResult, error) {
	config, err := s.store.GetAmneziaAPINodeConfig(connection.NodeID)
	if err != nil {
		return ConnectionResult{}, err
	}
	provider := amnezia.New(config.BaseURL, config.APIKey)
	created, err := provider.CreateConnection(ctx, remote.CreateInput{AccountID: connection.AccountID, ConnectionID: connection.ID, NodeID: connection.NodeID, Name: connection.Name})
	if err != nil {
		return ConnectionResult{}, err
	}
	connection.AppliedStatus = model.ConnectionStatusActive
	connection.DesiredVersionMS = time.Now().UTC().UnixMilli()
	connection.AppliedVersionMS = connection.DesiredVersionMS
	configs := make([]model.IssuedConfig, 0, len(created.Configs))
	for _, item := range created.Configs {
		configs = append(configs, model.IssuedConfig{Kind: item.Kind, Slug: item.Slug, Name: ConfigName(input.Node, connection.Name), Client: item.Client, ContentType: item.ContentType, Config: item.Value})
	}
	bundle, err := s.store.CreateConnectionBundle(model.ConnectionBundle{
		Connection: connection,
		AmneziaAPIRef: &model.AmneziaAPIConnectionRef{
			RemoteID: created.Ref.ID, RemoteName: created.Ref.Name,
		},
		Configs: configs,
	})
	if err != nil {
		_ = provider.DeleteConnection(ctx, created.Ref)
		return ConnectionResult{}, err
	}
	return ConnectionResult{Connection: bundle.Connection, Configs: bundle.Configs}, nil
}

func (s *Service) SetConnectionStatus(ctx context.Context, connection model.Connection, node model.Node, desired model.ConnectionStatus, _ string) (model.Connection, error) {
	if desired != model.ConnectionStatusActive && desired != model.ConnectionStatusHeld {
		return model.Connection{}, errors.New("unsupported connection status")
	}
	if node.Type == model.NodeTypeAmneziaAPI {
		if err := s.applyExternalStatus(ctx, connection, node, desired, false); err != nil {
			return model.Connection{}, err
		}
	}
	updated, err := s.store.UpdateConnectionDesiredStatus(connection.ID, desired)
	if err != nil {
		return model.Connection{}, err
	}
	if node.Type == model.NodeTypeAmneziaAPI {
		finalized, err := s.store.FinalizeConnectionsForAuthSnapshot(node.ID, connection.AccountID, updated.DesiredVersionMS)
		if err != nil || len(finalized) == 0 {
			return model.Connection{}, err
		}
		return finalized[0], nil
	}
	return updated, s.enqueueAuthRefresh(ctx, updated)
}

func (s *Service) DeleteConnection(ctx context.Context, connection model.Connection, node model.Node) (model.Connection, error) {
	if node.Type == model.NodeTypeAmneziaAPI {
		if err := s.applyExternalStatus(ctx, connection, node, model.ConnectionStatusDeleted, true); err != nil {
			return model.Connection{}, err
		}
	}
	updated, err := s.store.UpdateConnectionDesiredStatus(connection.ID, model.ConnectionStatusDeleted)
	if err != nil {
		return model.Connection{}, err
	}
	if node.Type == model.NodeTypeAmneziaAPI {
		finalized, err := s.store.FinalizeConnectionsForAuthSnapshot(node.ID, connection.AccountID, updated.DesiredVersionMS)
		if err != nil || len(finalized) == 0 {
			return model.Connection{}, err
		}
		return finalized[0], nil
	}
	return updated, s.enqueueAuthRefresh(ctx, updated)
}

func (s *Service) applyExternalStatus(ctx context.Context, connection model.Connection, node model.Node, status model.ConnectionStatus, deleting bool) error {
	config, err := s.store.GetAmneziaAPINodeConfig(node.ID)
	if err != nil {
		return err
	}
	ref, err := s.store.GetAmneziaAPIConnectionRef(connection.ID)
	if err != nil {
		return err
	}
	provider := amnezia.New(config.BaseURL, config.APIKey)
	remoteRef := remote.Ref{ID: ref.RemoteID, Name: ref.RemoteName}
	if deleting {
		return provider.DeleteConnection(ctx, remoteRef)
	}
	return provider.SetConnectionStatus(ctx, remoteRef, status)
}

func (s *Service) enqueueAuthRefresh(ctx context.Context, connection model.Connection) error {
	return s.transport.Enqueue(ctx, workqueue.NewAuthRefreshMessage(workqueue.AuthRefreshPayload{NodeID: connection.NodeID, AccountID: connection.AccountID, SnapshotVersionMS: connection.DesiredVersionMS}))
}

func (s *Service) SelectEnrollmentNodes(nodeIDs []string) ([]model.Node, error) {
	byID := map[string]model.Node{}
	for _, node := range s.store.ListNodes() {
		byID[node.ID] = node
	}
	if len(nodeIDs) == 0 {
		return nil, errors.New("node_ids are required")
	}
	var nodes []model.Node
	seen := map[string]bool{}
	for _, id := range nodeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		node, ok := byID[id]
		if !ok || node.Status != model.StatusActive || EnrollmentProtocolForNode(node) == "" {
			return nil, fmt.Errorf("node %s is unavailable", id)
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("no nodes selected")
	}
	return nodes, nil
}

func (s *Service) ListRemoteConnections(ctx context.Context, node model.Node) (remote.ConnectionList, error) {
	if node.Type != model.NodeTypeAmneziaAPI {
		var items []remote.ConnectionInfo
		for _, connection := range s.store.ListConnections() {
			if connection.NodeID == node.ID {
				items = append(items, remote.ConnectionInfo{Ref: remote.Ref{ID: connection.ID, Name: connection.Name}, Name: connection.Name, Status: string(connection.DesiredStatus)})
			}
		}
		return remote.ConnectionList{Total: len(items), Items: items}, nil
	}
	config, err := s.store.GetAmneziaAPINodeConfig(node.ID)
	if err != nil {
		return remote.ConnectionList{}, err
	}
	return amnezia.New(config.BaseURL, config.APIKey).ListConnections(ctx)
}

func EnrollmentProtocolForNode(node model.Node) model.Protocol {
	switch node.Type {
	case model.NodeTypeAmneziaAPI, model.NodeTypeNativeAmneziaWG:
		return model.ProtocolAmneziaWG
	case model.NodeTypeNativeHy2:
		return model.ProtocolHysteria2
	default:
		return ""
	}
}

func ConfigName(node model.Node, connectionName string) string {
	parts := []string{}
	if name := strings.TrimSpace(connectionName); name != "" {
		parts = append(parts, name)
	}
	regionCode := strings.ToUpper(strings.TrimSpace(node.Region))
	// Older nodes may store a location suffix, for example NL-Amsterdam.
	if i := strings.IndexAny(regionCode, "-_/ "); i >= 0 {
		regionCode = regionCode[:i]
	}
	if region, err := language.ParseRegion(regionCode); err == nil && region.IsCountry() && len(regionCode) == 2 {
		parts = append(parts, string([]rune{0x1F1E6 + rune(regionCode[0]-'A'), 0x1F1E6 + rune(regionCode[1]-'A')}))
	}
	if name := strings.TrimSpace(node.Name); name != "" {
		parts = append(parts, name)
	}
	return strings.Join(parts, " ")
}

func nextAvailableIPv4(config model.AmneziaWGNodeConfig, materials []model.AmneziaWGConnectionMaterial) (string, error) {
	first, last := net.ParseIP(config.FirstClientIPv4).To4(), net.ParseIP(config.LastClientIPv4).To4()
	if first == nil || last == nil || compareIP(first, last) > 0 {
		return "", errors.New("invalid amneziawg IPv4 pool")
	}
	used := map[string]bool{}
	used[config.ServerIPv4] = true
	for _, material := range materials {
		used[material.AssignedIPv4] = true
	}
	for current := append(net.IP(nil), first...); compareIP(current, last) <= 0; current = nextIP(current) {
		if !used[current.String()] {
			return current.String(), nil
		}
	}
	return "", errors.New("amneziawg IPv4 pool exhausted")
}

func compareIP(a, b net.IP) int {
	for i := 0; i < net.IPv4len; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func nextIP(ip net.IP) net.IP {
	out := append(net.IP(nil), ip...)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}
