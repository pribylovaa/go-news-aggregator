// config предоставляет структуру конфигурации user-service
// и функции загрузки из YAML/ENV с предсказуемым приоритетом.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config — корневая конфигурация сервиса.
type Config struct {
	Env      string         `yaml:"env" env:"ENV" env-default:"local"`
	GRPC     GRPCConfig     `yaml:"grpc"`
	Postgres PostgresConfig `yaml:"postgres"`
	S3       S3Config       `yaml:"s3"`
	Avatar   AvatarConfig   `yaml:"avatar"`
	Timeouts TimeoutConfig  `yaml:"timeouts"`
}

// GRPCConfig — сетевые настройки gRPC-сервера.
type GRPCConfig struct {
	Host string `yaml:"host" env:"GRPC_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"GRPC_PORT" env-default:"50053"`
}

// Addr возвращает адрес в формате host:port.
func (g GRPCConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

type PostgresConfig struct {
	URL string `yaml:"url" env:"POSTGRES" env-required:"true"`
}

type S3Config struct {
	Endpoint     string        `yaml:"endpoint" env:"S3_ENDPOINT" env-required:"true"`
	RootUser     string        `yaml:"root_user" env:"S3_ROOT_USER" env-required:"true"`
	RootPassword string        `yaml:"root_password" env:"S3_ROOT_PASSWORD" env-required:"true"`
	Bucket       string        `yaml:"bucket" env:"S3_BUCKET" env-required:"true"`
	PresignTTL   time.Duration `yaml:"presign_ttl" env:"S3_PRESIGN_TTL" env-default:"10m"`
}

type AvatarConfig struct {
	MaxSizeBytes        int64    `yaml:"max_size_bytes" env:"AVATAR_MAX_SIZE_BYTES" env-default:"5242880"`
	AllowedContentTypes []string `yaml:"allowed_content_types" env:"AVATAR_ALLOWED_CONTENT_TYPES" env-separator:"," env-default:"image/jpeg,image/png"`
}

// TimeoutConfig — таймауты сервиса.
type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env:"SERVICE_TIMEOUT" env-default:"5s"`
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
// 1) явный путь; 2) CONFIG_PATH; 3) ./local.yaml; 4) ENV.
func Load(path string) (*Config, error) {
	var cfg Config

	tryRead := func(p string) (*Config, error) {
		if p == "" {
			return nil, fmt.Errorf("empty config path")
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("config file %q stat failed: %w", p, err)
		}
		if err := cleanenv.ReadConfig(p, &cfg); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		return &cfg, nil
	}

	// 1) Явный путь.
	if path != "" {
		c, err := tryRead(path)
		if err != nil {
			return nil, err
		}
		if err := c.validate(); err != nil {
			return nil, err
		}
		return c, nil
	}

	// 2) CONFIG_PATH.
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		c, err := tryRead(envPath)
		if err != nil {
			return nil, err
		}
		if err := c.validate(); err != nil {
			return nil, err
		}
		return c, nil
	}

	// 3) ./local.yaml.
	if _, err := os.Stat("local.yaml"); err == nil {
		if err := cleanenv.ReadConfig("local.yaml", &cfg); err != nil {
			return nil, fmt.Errorf("failed to read local.yaml: %w", err)
		}
		if err := cfg.validate(); err != nil {
			return nil, err
		}
		return &cfg, nil
	}

	// 4) Только ENV.
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("config not found: provide --config, CONFIG_PATH, local.yaml or env vars: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.S3.PresignTTL == 0 {
		c.S3.PresignTTL = 10 * time.Minute
	}

	if c.Avatar.MaxSizeBytes == 0 {
		c.Avatar.MaxSizeBytes = 5 * 1024 * 1024 // 5 MiB
	}

	if c.Postgres.URL == "" {
		return fmt.Errorf("postgres.url is required")
	}

	if c.GRPC.Host == "" {
		return fmt.Errorf("grpc.host is required")
	}

	if c.GRPC.Port == "" {
		return fmt.Errorf("grpc.port is required")
	}

	if p, err := strconv.Atoi(c.GRPC.Port); err != nil || p <= 0 || p > 65535 {
		return fmt.Errorf("grpc.port must be a valid TCP port (1..65535)")
	}

	if c.S3.Endpoint == "" {
		return fmt.Errorf("s3.endpoint is required")
	}

	if c.S3.RootUser == "" {
		return fmt.Errorf("s3.root_user is required")
	}

	if c.S3.RootPassword == "" {
		return fmt.Errorf("s3.root_password is required")
	}

	if c.S3.Bucket == "" {
		return fmt.Errorf("s3.bucket is required")
	}

	if c.S3.PresignTTL < 0 {
		return fmt.Errorf("s3.presign_ttl must be >= 0")
	}

	if c.Avatar.MaxSizeBytes < 0 {
		return fmt.Errorf("avatar.max_size_bytes must be >= 0")
	}

	if len(c.Avatar.AllowedContentTypes) == 0 {
		return fmt.Errorf("avatar.allowed_content_types must not be empty")
	}

	return nil
}
