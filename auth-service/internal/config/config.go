package config

import (
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env      string        `yaml:"env" env-default:"local"`
	GRPC     GRPCConfig    `yaml:"grpc"`
	Auth     AuthConfig    `yaml:"auth"`
	DB       DBConfig      `yaml:"db"`
	Timeouts TimeoutConfig `yaml:"timeouts"`
}

type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env-default:"5s"`
}

type GRPCConfig struct {
	Host string `yaml:"host" env-default:"0.0.0.0"`
	Port string `yaml:"port" env-default:"50051"`
}

type AuthConfig struct {
	JWTSecret       string        `yaml:"jwt_secret" env-required:"true"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env-default:"15m"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env-default:"720h"`
	Issuer          string        `yaml:"issuer"   env-default:"auth-service"`
	Audience        []string      `yaml:"audience" env-default:"api-gateway"`
}

type DBConfig struct {
	DatabaseURL string `yaml:"db_url" env-required:"true"`
}

// MustLoad — обёртка над Load с panic при ошибке.
func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic(err)
	}

	return cfg
}

// Load загружает конфигурацию по приоритету:
// 1) явный путь (аргумент функции, передаётся из main через флаг --config);
// 2) переменная окружения CONFIG_PATH;
// 3) файл ./local.yaml (удобный дефолт для dev);
// 4) иначе - только из переменных окружения.
func Load(path string) (*Config, error) {
	var cfg Config

	tryRead := func(p string) (*Config, error) {
		if p == "" {
			return nil, fmt.Errorf("empty config path")
		}

		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("config file does not exist: %s", p)
		}

		if err := cleanenv.ReadConfig(p, &cfg); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}

		return &cfg, nil
	}

	if path != "" {
		return tryRead(path)
	}

	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		return tryRead(envPath)
	}

	if _, err := os.Stat("local.yaml"); err == nil {
		if err := cleanenv.ReadConfig("local.yaml", &cfg); err != nil {
			return nil, fmt.Errorf("failed to read local.yaml: %w", err)
		}

		return &cfg, nil
	}

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("config not found: provide --config, CONFIG_PATH, local.yaml or env vars: %w", err)
	}

	return &cfg, nil
}
