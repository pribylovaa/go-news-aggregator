// config - источник загрузки конфигурации для API Gateway.
//
// Источники (по убыванию приоритета):
//  1. явный путь --config;
//  2. CONFIG_PATH;
//  3. ./local.yaml;
//  4. только ENV (cleanenv).
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env      string        `yaml:"env" env:"ENV" env-default:"local"`
	HTTP     HTTPConfig    `yaml:"http"`
	GRPC     GRPCConfig    `yaml:"grpc"`
	Metrics  MetricsConfig `yaml:"metrics"`
	Timeouts TimeoutConfig `yaml:"timeouts"`
}

// TimeoutConfig — таймаут сервиса.
type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env:"SERVICE" env-default:"15s"`
}

// HTTPConfig — публичный REST-сервер шлюза.
type HTTPConfig struct {
	Host string `yaml:"host" env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"HTTP_PORT" env-default:"50090"`
}

func (h HTTPConfig) Addr() string { return net.JoinHostPort(h.Host, h.Port) }

// MetricsConfig — отдельный HTTP для Prometheus.
type MetricsConfig struct {
	Host string `yaml:"host"   env:"METRICS_HOST"   env-default:"0.0.0.0"`
	Port string `yaml:"port"   env:"METRICS_PORT"   env-default:"50085"`
}

func (m MetricsConfig) Addr() string { return net.JoinHostPort(m.Host, m.Port) }

// GRPCConfig — адреса внутренних gRPC-сервисов.
type GRPCConfig struct {
	AuthAddr     string `yaml:"auth_addr"     env:"GRPC_AUTH_ADDR"     env-default:"0.0.0.0:50081"`
	NewsAddr     string `yaml:"news_addr"     env:"GRPC_NEWS_ADDR"     env-default:"0.0.0.0:50082"`
	UsersAddr    string `yaml:"users_addr"    env:"GRPC_USERS_ADDR"    env-default:"0.0.0.0:50083"`
	CommentsAddr string `yaml:"comments_addr" env:"GRPC_COMMENTS_ADDR" env-default:"0.0.0.0:50084"`
}

// MustLoad — паника при ошибке загрузки.
func MustLoad(path string) *Config {
	cfg, err := Load(path)

	if err != nil {
		panic(err)
	}

	return cfg
}

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

		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("failed to overlay env: %w", err)
		}

		return &cfg, nil
	}

	// 1) --config
	if path != "" {
		return tryRead(path)
	}

	// 2) CONFIG_PATH
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		return tryRead(envPath)
	}

	// 3) ./local.yaml
	if _, err := os.Stat("local.yaml"); err == nil {
		if err := cleanenv.ReadConfig("local.yaml", &cfg); err != nil {
			return nil, fmt.Errorf("failed to read local.yaml: %w", err)
		}

		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("failed to overlay env: %w", err)
		}

		return &cfg, nil
	}

	// 4) только ENV
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("config not found: provide --config, CONFIG_PATH, local.yaml or env vars: %w", err)
	}
	return &cfg, nil
}
