package record

import "time"

type Account struct {
	ID          string
	Username    string
	DisplayName string
	Status      string
	Note        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Client struct {
	ID        string
	AccountID string
	Slug      string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Node struct {
	ID         string
	Name       string
	Type       string
	BaseURL    string
	APIKey     string
	Region     string
	SSHHost    string
	SSHPort    int
	SSHUser    string
	SSHKeyPath string
	UseIPv6    bool
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Connection struct {
	ID            string
	AccountID     string
	ClientID      string
	NodeID        string
	Protocol      string
	RemoteID      string
	RemoteName    string
	Status        string
	DesiredStatus string
	LastSyncAt    time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type IssuedConfig struct {
	ID           string
	ConnectionID string
	Kind         string
	Slug         string
	Name         string
	Client       string
	ContentType  string
	Config       string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ConfigProfile struct {
	ID             string
	Protocol       string
	Kind           string
	Slug           string
	Name           string
	Client         string
	ContentType    string
	ConfigTemplate string
	Status         string
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
	Status     string
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
	Status        string
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
	Status     string
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
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AccountPolicyTag struct {
	ID        string
	AccountID string
	TagID     string
	CreatedAt time.Time
}

type ShortLink struct {
	ID           string
	TokenID      string
	Profile      string
	TargetURL    string
	EncryptedURL string
	Status       string
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
