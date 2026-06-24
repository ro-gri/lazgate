package model

import "time"

type Status string

type AccountStatus string

const (
	AccountStatusActive  AccountStatus = "active"
	AccountStatusHeld    AccountStatus = "held"
	AccountStatusDeleted AccountStatus = "deleted"
)

type ConnectionStatus string

const (
	ConnectionStatusActive  ConnectionStatus = "active"
	ConnectionStatusAbsent  ConnectionStatus = "absent"
	ConnectionStatusHeld    ConnectionStatus = "held"
	ConnectionStatusDeleted ConnectionStatus = "deleted"
	ConnectionStatusError   ConnectionStatus = "error"
)

type RuntimeCommandStatus string

const (
	RuntimeCommandStatusActive    RuntimeCommandStatus = "active"
	RuntimeCommandStatusCompleted RuntimeCommandStatus = "completed"
	RuntimeCommandStatusError     RuntimeCommandStatus = "error"
)

const (
	StatusActive  Status = "active"
	StatusAbsent  Status = "absent"
	StatusHeld    Status = "held"
	StatusDeleted Status = "deleted"
	StatusError   Status = "error"
)

type NodeType string

const (
	NodeTypeAmneziaAPI      NodeType = "amnezia_api"
	NodeTypeNativeHy2       NodeType = "native_hysteria"
	NodeTypeNativeAmneziaWG NodeType = "native_amneziawg"
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
	ConnectionLimitUnlimited = -1
)

type Account struct {
	ID          string
	Username    string
	DisplayName string
	Status      AccountStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Node struct {
	ID        string
	Name      string
	Type      NodeType
	Region    string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NodeInstance struct {
	ID          string
	NodeID      string
	Host        string
	SSHPort     int
	SSHUser     string
	SSHKey      string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ActivatedAt time.Time
	RetiredAt   time.Time
}

type AgentInstallation struct {
	ID                           string
	NodeInstanceID               string
	AgentType                    string
	Status                       Status
	CertificateFingerprintSHA256 string
	CertificateSerial            string
	CertificateNotBefore         time.Time
	CertificateNotAfter          time.Time
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type Connection struct {
	ID               string
	AccountID        string
	NodeID           string
	Name             string
	Protocol         Protocol
	DesiredStatus    ConnectionStatus
	AppliedStatus    ConnectionStatus
	DesiredVersionMS int64
	AppliedVersionMS int64
	LastErrorID      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ConnectionBundle struct {
	ExpectedNodeConfigVersionMS int64
	Connection                  Connection
	Hysteria2Material           *Hysteria2ConnectionMaterial
	AmneziaWGMaterial           *AmneziaWGConnectionMaterial
	AmneziaAPIRef               *AmneziaAPIConnectionRef
	Configs                     []IssuedConfig
}

func (c Connection) Pending() bool {
	return c.DesiredStatus != c.AppliedStatus || c.DesiredVersionMS > c.AppliedVersionMS
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

type Hysteria2NodeConfig struct {
	MasqueradeFileSetID    string
	SettingsJSON           string
	AppliedConfigVersionMS int64
	ConfigErrorID          string
	NodeID                 string
	EndpointHost           string
	EndpointPort           int
	ObfsEnabled            bool
	ObfsType               string
	ObfsPassword           string
	UseIPv6                bool
	MasqueradeURL          string
	ConfigVersionMS        int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type HysteriaFileSet struct {
	ID           string
	Archive      string
	ManifestJSON string
}

type Hysteria2ConfigSource struct {
	ConnectionID string
	Name         string
	Password     string
}

type AmneziaAPINodeConfig struct {
	NodeID    string
	BaseURL   string
	APIKey    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AmneziaAPIConnectionRef struct {
	ConnectionID string
	RemoteID     string
	RemoteName   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AmneziaWGNodeConfig struct {
	NodeID                 string
	EndpointHost           string
	EndpointPort           int
	ListenPort             int
	ServerIPv4             string
	ClientIPv4CIDR         string
	FirstClientIPv4        string
	LastClientIPv4         string
	DNS                    string
	MTU                    int
	PersistentKeepalive    int
	ClientRouteAllowedIPs  string
	ClientRouteMode        string
	ClientExcludedRoutes   string
	ServerPrivateKey       string
	HeaderProtectionKey    string
	ContentPaddingAddition string
	TimingsJSON            string
	RandomTrailers         bool
	DisableCookies         bool
	AppliedConfigVersionMS int64
	ConfigErrorID          string
	ServerPublicKey        string
	JC                     int
	JMin                   int
	JMax                   int
	S1                     int
	S2                     int
	H1                     string
	H2                     string
	H3                     string
	H4                     string
	S3                     int
	S4                     int
	I1                     string
	I2                     string
	I3                     string
	I4                     string
	I5                     string
	ConfigVersionMS        int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type Hysteria2ConnectionMaterial struct {
	ConnectionID string
	Password     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AmneziaWGConnectionMaterial struct {
	ConnectionID     string
	NodeID           string
	ClientPublicKey  string
	ClientPrivateKey string
	PresharedKey     string
	AssignedIPv4     string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AmneziaWGSyncMaterial struct {
	NodeConfig AmneziaWGNodeConfig
	Connection Connection
	Material   AmneziaWGConnectionMaterial
}

type AuthIdentityKind string

const (
	AuthIdentityKindAdmin AuthIdentityKind = "admin"
	AuthIdentityKindUser  AuthIdentityKind = "user"
)

type AuthCredentialType string

const (
	AuthCredentialTypePassword AuthCredentialType = "password"
)

type AdminRole string

type AdminInviteStatus string

const (
	AdminRoleOwner  AdminRole = "owner"
	AdminRoleAdmin  AdminRole = "admin"
	AdminRoleViewer AdminRole = "viewer"
)

const (
	AdminInviteStatusActive  AdminInviteStatus = "active"
	AdminInviteStatusUsed    AdminInviteStatus = "used"
	AdminInviteStatusRevoked AdminInviteStatus = "revoked"
)

type AdminInvite struct {
	ID                     string
	TokenHash              string
	CodeHash               string
	Role                   AdminRole
	Status                 AdminInviteStatus
	ExpiresAt              time.Time
	InvitedByIdentityID    string
	InvitedByLoginSnapshot string
	UsedByIdentityID       string
	UsedByLogin            string
	UsedByDisplayName      string
	UsedAt                 time.Time
	RevokedAt              time.Time
	Note                   string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type AuthIdentity struct {
	ID                 string
	Kind               AuthIdentityKind
	Login              string
	DisplayName        string
	AccountID          string
	Status             Status
	MustChangePassword bool
	VerifiedAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AuthCredential struct {
	ID             string
	IdentityID     string
	Type           AuthCredentialType
	SecretHash     string
	FailedAttempts int
	LockedUntil    time.Time
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AuthSession struct {
	ID                   string
	IdentityID           string
	AccountID            string
	TokenHash            string
	RefreshTokenHash     string
	RefreshExpiresAt     time.Time
	RefreshAbsoluteUntil time.Time
	Status               Status
	ExpiresAt            time.Time
	LastUsedAt           time.Time
	LastSeenAt           time.Time
	OnlineUntil          time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AuthRefreshTokenStatus string

const (
	AuthRefreshTokenStatusActive  AuthRefreshTokenStatus = "active"
	AuthRefreshTokenStatusRotated AuthRefreshTokenStatus = "rotated"
	AuthRefreshTokenStatusRevoked AuthRefreshTokenStatus = "revoked"
	AuthRefreshTokenStatusReused  AuthRefreshTokenStatus = "reused"
)

type AuthRefreshToken struct {
	ID                string
	TokenHash         string
	SessionID         string
	IdentityID        string
	FamilyID          string
	Status            AuthRefreshTokenStatus
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	RotatedAt         time.Time
	LastUsedAt        time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type RefreshTokenRotation struct {
	Session           AuthSession
	RefreshToken      AuthRefreshToken
	GraceRefreshToken AuthRefreshToken
	Grace             bool
}

type AuthRecoveryCode struct {
	ID         string
	IdentityID string
	CodeHash   string
	Status     Status
	UsedAt     time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AdminProfile struct {
	ID         string
	IdentityID string
	Role       AdminRole
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AdminIdentityProfile struct {
	Identity    AuthIdentity
	Profile     AdminProfile
	LastSeenAt  time.Time
	OnlineUntil time.Time
	Online      bool
}

type AccountPortalToken struct {
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

type ConnectionSubscriptionToken struct {
	ID           string
	ConnectionID string
	Token        string
	TokenHash    string
	Status       Status
	ExpiresAt    time.Time
	LastUsedAt   time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SessionView struct {
	ID        string
	AccountID string
	Status    Status
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PolicyTag struct {
	ID              string
	Slug            string
	Name            string
	AllowedNodeIDs  []string
	ConnectionLimit int
	Status          Status
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AccountPolicyTag struct {
	ID        string
	AccountID string
	TagID     string
	CreatedAt time.Time
}

type EffectiveConnectionPolicy struct {
	AllowedNodeIDs  []string
	ConnectionLimit int
}

type ShortLink struct {
	ID                  string
	SubscriptionTokenID string
	Profile             string
	EncryptedURL        string
	Status              Status
	CreatedAt           time.Time
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
	AgentInstallationID string
	NodeInstanceID      string
	AgentStatus         string
	LastHeartbeatAt     time.Time
	AgentVersion        string
	ProtocolVersion     string
	ServiceStatus       string
	RecentMessage       string
	UpdatedAt           time.Time
}

type NodeStatusInterval struct {
	ID                  string
	AgentInstallationID string
	NodeInstanceID      string
	Status              string
	StartedAtMS         int64
	EndedAtMS           int64
}

type NodeOnlineConnection struct {
	NodeInstanceID string
	ConnectionID   string
	NativeAuthID   string
	Count          int
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
}

type UsageRecord struct {
	RequestID      string
	ConnectionID   string
	NodeInstanceID string
	FromMS         int64
	ToMS           int64
	TXBytes        int64
	RXBytes        int64
}

type ConnectionUsageTotal struct {
	ConnectionID string
	TXBytes      int64
	RXBytes      int64
	UpdatedAtMS  int64
}

type NodeConnectionUsage struct {
	ConnectionID   string
	AccountName    string
	ConnectionName string
	FromMS         int64
	ToMS           int64
	TXBytes        int64
	RXBytes        int64
}

type NodeUsageSummary struct {
	TXBytes int64
	RXBytes int64
	Items   []NodeConnectionUsage
}

func (r UsageRecord) TotalBytes() int64 { return r.TXBytes + r.RXBytes }

type RuntimeCommand struct {
	ID                  string
	AgentInstallationID string
	Type                string
	Payload             string
	Status              RuntimeCommandStatus
	Result              string
	Error               string
	IssuedAt            time.Time
	ExpiresAt           time.Time
	UpdatedAt           time.Time
}

type AccountSummary struct {
	Account     Account
	Connections []ConnectionSummary
	Configs     []IssuedConfig
	Profiles    []ConfigProfile
	Generated   time.Time
}

type ConnectionSummary struct {
	Connection Connection
	Node       Node
}
