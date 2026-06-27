package model

import "time"

type Status string

const (
	StatusActive  Status = "active"
	StatusHeld    Status = "held"
	StatusDeleted Status = "deleted"
	StatusError   Status = "error"

	StatusPendingCreate Status = "pending_create"
	StatusPendingHold   Status = "pending_hold"
	StatusPendingResume Status = "pending_resume"
	StatusPendingDelete Status = "pending_delete"
)

type NodeType string

const (
	NodeTypeAmneziaAPI    NodeType = "amnezia_api"
	NodeTypeBlitzHysteria NodeType = "blitz_hysteria"
	NodeTypeNativeHy2     NodeType = "native_hysteria"
)

type Protocol string

const (
	ProtocolHysteria2 Protocol = "hysteria2"
	ProtocolAmneziaWG Protocol = "amneziawg"
)

type ConfigKind string

const (
	ConfigHy2URI      ConfigKind = "hy2_uri"
	ConfigAmneziaVPN  ConfigKind = "amnezia_vpn"
	ConfigAmneziaConf ConfigKind = "amnezia_conf"
	ConfigSingBoxJSON ConfigKind = "singbox_json"
)

const (
	TokenPurposeClient = "client"
)

const (
	ClientLimitUnlimited = -1
)

type Account struct {
	ID          string
	Username    string
	DisplayName string
	Status      Status
	Note        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Client struct {
	ID        string
	AccountID string
	Slug      string
	Name      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Node struct {
	ID         string
	Name       string
	Type       NodeType
	BaseURL    string
	APIKey     string
	Region     string
	SSHHost    string
	SSHPort    int
	SSHUser    string
	SSHKeyPath string
	UseIPv6    bool
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Connection struct {
	ID            string
	AccountID     string
	ClientID      string
	NodeID        string
	Protocol      Protocol
	RemoteID      string
	RemoteName    string
	Status        Status
	DesiredStatus Status
	LastSyncAt    time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type IssuedConfig struct {
	ID           string
	ConnectionID string
	Kind         ConfigKind
	Slug         string
	Name         string
	Client       string
	ContentType  string
	Config       string
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ConfigProfile struct {
	ID             string
	Protocol       Protocol
	Kind           ConfigKind
	Slug           string
	Name           string
	Client         string
	ContentType    string
	ConfigTemplate string
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AccessToken struct {
	ID         string
	AccountID  string
	ClientID   string
	Token      string
	TokenHash  string
	Purpose    string
	Status     Status
	ExpiresAt  time.Time
	LastUsedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AdminSession struct {
	ID            string
	Token         string
	TokenHash     string
	CSRFToken     string
	CSRFTokenHash string
	PrincipalName string
	Role          string
	Status        Status
	ExpiresAt     time.Time
	LastUsedAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ClientCredential struct {
	AccountID        string
	PINHash          string
	RecoveryCodeHash string
	FailedAttempts   int
	LockedUntil      time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ClientSession struct {
	ID         string
	AccountID  string
	Token      string
	TokenHash  string
	Status     Status
	ExpiresAt  time.Time
	LastUsedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type PolicyTag struct {
	ID             string
	Slug           string
	Name           string
	AllowedNodeIDs []string
	ClientLimit    int
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AccountPolicyTag struct {
	ID        string
	AccountID string
	TagID     string
	CreatedAt time.Time
}

type EffectiveClientPolicy struct {
	AllowedNodeIDs []string
	ClientLimit    int
}

type ShortLink struct {
	ID           string
	TokenID      string
	Profile      string
	TargetURL    string
	EncryptedURL string
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AuditLog struct {
	ID         string
	Actor      string
	Action     string
	EntityType string
	EntityID   string
	Details    string
	CreatedAt  time.Time
}

type EventStatus string

const (
	EventPending   EventStatus = "pending"
	EventDelivered EventStatus = "delivered"
	EventExpired   EventStatus = "expired"
)

type Event struct {
	ID            string
	Topic         string
	Status        EventStatus
	Type          string
	EntityType    string
	EntityID      string
	Actor         string
	Message       string
	PayloadJSON   string
	CreatedAtMS   int64
	DeliveredAtMS int64
	ExpiresAtMS   int64
}

type NodeRuntime struct {
	NodeID                     string
	AgentStatus                string
	LastHeartbeatAt            time.Time
	AgentVersion               string
	ProtocolVersion            string
	HysteriaServiceStatus      string
	LastTrafficCollectionAt    time.Time
	LastOnlineCollectionAt     time.Time
	PendingUsageBatchCount     int
	PendingUsageQueueSizeBytes int64
	RecentMessage              string
	UpdatedAt                  time.Time
}

type NodeStatusInterval struct {
	ID          string
	NodeID      string
	Status      string
	StartedAtMS int64
	EndedAtMS   int64
}

type NodeOnlineClient struct {
	NodeID       string
	CredentialID string
	Count        int
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
}

type UsageBatch struct {
	BatchID      string
	NodeID       string
	FromMS       int64
	ToMS         int64
	ReceivedAtMS int64
}

type UsageRecord struct {
	ID           string
	BatchID      string
	NodeID       string
	CredentialID string
	FromMS       int64
	ToMS         int64
	TXBytes      int64
	RXBytes      int64
	TotalBytes   int64
	ReceivedAtMS int64
}

type RuntimeCommand struct {
	ID        string
	NodeID    string
	Type      string
	Payload   string
	Status    Status
	Result    string
	Error     string
	IssuedAt  time.Time
	ExpiresAt time.Time
	UpdatedAt time.Time
}

type AccountSummary struct {
	Account     Account
	Clients     []Client
	Connections []ConnectionSummary
	Configs     []IssuedConfig
	Profiles    []ConfigProfile
	Generated   time.Time
}

type ConnectionSummary struct {
	Connection Connection
	Node       Node
	Client     Client
}
