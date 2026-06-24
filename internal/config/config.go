package config

import env "github.com/caarlos0/env/v11"

type Config struct {
	Addr             string `env:"LAZ_ADDR" envDefault:"127.0.0.1:8088"`
	Name             string `env:"LAZ_NAME" envDefault:"Chamomile"`
	Storage          string `env:"LAZ_STORAGE" envDefault:"sqlite"`
	DataPath         string `env:"LAZ_DATA" envDefault:"./data/laz.db"`
	DatabaseURL      string `env:"LAZ_DATABASE_URL"`
	SecretKey        string `env:"LAZ_SECRET_KEY"`
	AdminToken       string `env:"LAZ_ADMIN_TOKEN" envDefault:"change-me"`
	AdminTokenSHA256 string `env:"LAZ_ADMIN_TOKEN_SHA256"`
	PublicBaseURL    string `env:"LAZ_PUBLIC_BASE_URL"`
	WebPrefix        string `env:"LAZ_WEB_PREFIX" envDefault:"/admin"`
	BlankPagePath    string `env:"LAZ_BLANK_PAGE_PATH"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
