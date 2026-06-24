package record

import "time"

type Account struct {
	ID          string
	Username    string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Node struct {
	ID        string
	Name      string
	Type      string
	Region    string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Connection struct {
	ID               string
	AccountID        string
	NodeID           string
	Name             string
	Protocol         string
	DesiredStatus    string
	AppliedStatus    string
	DesiredVersionMS int64
	AppliedVersionMS int64
	LastErrorID      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
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

type AuthIdentity struct {
	ID                 string
	Kind               string
	Login              string
	DisplayName        string
	AccountID          string
	Status             string
	MustChangePassword bool
	VerifiedAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AuthCredential struct {
	ID             string
	IdentityID     string
	Type           string
	SecretHash     string
	FailedAttempts int
	LockedUntil    time.Time
	Status         string
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
	Status               string
	ExpiresAt            time.Time
	LastUsedAt           time.Time
	LastSeenAt           time.Time
	OnlineUntil          time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AuthRefreshToken struct {
	ID                string
	TokenHash         string
	SessionID         string
	IdentityID        string
	FamilyID          string
	Status            string
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	RotatedAt         time.Time
	LastUsedAt        time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AuthRecoveryCode struct {
	ID         string
	IdentityID string
	CodeHash   string
	Status     string
	UsedAt     time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AdminProfile struct {
	ID         string
	IdentityID string
	Role       string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AccountPortalToken struct {
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

type ConnectionSubscriptionToken struct {
	ID           string
	ConnectionID string
	Token        string
	TokenHash    string
	Status       string
	ExpiresAt    time.Time
	LastUsedAt   time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PolicyTag struct {
	ID              string
	Slug            string
	Name            string
	AllowedNodeIDs  []string
	ConnectionLimit int
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AccountPolicyTag struct {
	ID        string
	AccountID string
	TagID     string
	CreatedAt time.Time
}

type ShortLink struct {
	ID                  string
	SubscriptionTokenID string
	Profile             string
	EncryptedURL        string
	Status              string
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
