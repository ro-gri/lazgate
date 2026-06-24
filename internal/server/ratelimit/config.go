package ratelimit

// Config contains per-IP HTTP rate limits for public auth-sensitive routes.
type Config struct {
	Enabled                bool
	ConnectPerMinute       int
	VerifyPerMinute        int
	QRPerMinute            int
	HPLinkPerMinute        int
	PasswordSetupPerMinute int
	AdminLoginPerMinute    int
	UserLoginPerMinute     int
	RecoveryPerMinute      int
}

// DefaultConfig returns the conservative production defaults used before env overrides.
func DefaultConfig() Config {
	return Config{
		Enabled:                true,
		ConnectPerMinute:       30,
		VerifyPerMinute:        10,
		QRPerMinute:            10,
		HPLinkPerMinute:        10,
		PasswordSetupPerMinute: 5,
		AdminLoginPerMinute:    5,
		UserLoginPerMinute:     5,
		RecoveryPerMinute:      5,
	}
}

// Normalize fills unset per-route limits from defaults while preserving an explicit Enabled value.
func (c Config) Normalize() Config {
	defaults := DefaultConfig()
	if c == (Config{}) {
		return defaults
	}
	if c.ConnectPerMinute <= 0 {
		c.ConnectPerMinute = defaults.ConnectPerMinute
	}
	if c.VerifyPerMinute <= 0 {
		c.VerifyPerMinute = defaults.VerifyPerMinute
	}
	if c.QRPerMinute <= 0 {
		c.QRPerMinute = defaults.QRPerMinute
	}
	if c.HPLinkPerMinute <= 0 {
		c.HPLinkPerMinute = defaults.HPLinkPerMinute
	}
	if c.PasswordSetupPerMinute <= 0 {
		c.PasswordSetupPerMinute = defaults.PasswordSetupPerMinute
	}
	if c.AdminLoginPerMinute <= 0 {
		c.AdminLoginPerMinute = defaults.AdminLoginPerMinute
	}
	if c.UserLoginPerMinute <= 0 {
		c.UserLoginPerMinute = defaults.UserLoginPerMinute
	}
	if c.RecoveryPerMinute <= 0 {
		c.RecoveryPerMinute = defaults.RecoveryPerMinute
	}
	return c
}
