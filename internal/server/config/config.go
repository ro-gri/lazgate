package config

import (
	"laz/internal/server/ratelimit"

	env "github.com/caarlos0/env/v11"
)

type Config struct {
	Addr                 string `env:"LAZ_ADDR" envDefault:"127.0.0.1:8088"`
	Name                 string `env:"LAZ_NAME" envDefault:"Chamomile"`
	Storage              string `env:"LAZ_STORAGE" envDefault:"sqlite"`
	DataPath             string `env:"LAZ_DATA" envDefault:"./data/laz.db"`
	AuthDataPath         string `env:"LAZ_AUTH_DATA"`
	TransportDataPath    string `env:"LAZ_TRANSPORT_DATA"`
	DatabaseURL          string `env:"LAZ_DATABASE_URL"`
	AuthDatabaseURL      string `env:"LAZ_AUTH_DATABASE_URL"`
	TransportDatabaseURL string `env:"LAZ_TRANSPORT_DATABASE_URL"`
	SecretKey            string `env:"LAZ_SECRET_KEY"`
	OwnerLogin           string `env:"LAZ_OWNER_LOGIN"`
	OwnerPassword        string `env:"LAZ_OWNER_PASSWORD"`
	PublicBaseURL        string `env:"LAZ_PUBLIC_BASE_URL"`
	WebPrefix            string `env:"LAZ_WEB_PREFIX" envDefault:"/admin"`
	BlankPagePath        string `env:"LAZ_BLANK_PAGE_PATH"`
	AgentGRPCAddr        string `env:"LAZ_AGENT_GRPC_ADDR"`
	AgentGRPCTarget      string `env:"LAZ_AGENT_GRPC_TARGET"`
	AgentDownloadBase    string `env:"LAZ_AGENT_DOWNLOAD_BASE" envDefault:"https://github.com/ro-gri/lazgate/releases/latest/download"`
	AgentGRPCCAFile      string `env:"LAZ_AGENT_GRPC_CA_FILE"`
	AgentGRPCCertFile    string `env:"LAZ_AGENT_GRPC_CERT_FILE"`
	AgentGRPCKeyFile     string `env:"LAZ_AGENT_GRPC_KEY_FILE"`

	RateLimitEnabled                bool `env:"LAZ_RATE_LIMIT_ENABLED" envDefault:"true"`
	RateLimitConnectPerMinute       int  `env:"LAZ_RATE_LIMIT_CONNECT_PER_MINUTE" envDefault:"30"`
	RateLimitVerifyPerMinute        int  `env:"LAZ_RATE_LIMIT_VERIFY_PER_MINUTE" envDefault:"10"`
	RateLimitQRPerMinute            int  `env:"LAZ_RATE_LIMIT_QR_PER_MINUTE" envDefault:"10"`
	RateLimitHPLinkPerMinute        int  `env:"LAZ_RATE_LIMIT_HP_LINK_PER_MINUTE" envDefault:"10"`
	RateLimitPasswordSetupPerMinute int  `env:"LAZ_RATE_LIMIT_PASSWORD_SETUP_PER_MINUTE" envDefault:"5"`
	RateLimitAdminLoginPerMinute    int  `env:"LAZ_RATE_LIMIT_ADMIN_LOGIN_PER_MINUTE" envDefault:"5"`
	RateLimitUserLoginPerMinute     int  `env:"LAZ_RATE_LIMIT_USER_LOGIN_PER_MINUTE" envDefault:"5"`
	RateLimitRecoveryPerMinute      int  `env:"LAZ_RATE_LIMIT_RECOVERY_PER_MINUTE" envDefault:"5"`
}

func (c Config) RateLimits() ratelimit.Config {
	return ratelimit.Config{
		Enabled:                c.RateLimitEnabled,
		ConnectPerMinute:       c.RateLimitConnectPerMinute,
		VerifyPerMinute:        c.RateLimitVerifyPerMinute,
		QRPerMinute:            c.RateLimitQRPerMinute,
		HPLinkPerMinute:        c.RateLimitHPLinkPerMinute,
		PasswordSetupPerMinute: c.RateLimitPasswordSetupPerMinute,
		AdminLoginPerMinute:    c.RateLimitAdminLoginPerMinute,
		UserLoginPerMinute:     c.RateLimitUserLoginPerMinute,
		RecoveryPerMinute:      c.RateLimitRecoveryPerMinute,
	}.Normalize()
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
