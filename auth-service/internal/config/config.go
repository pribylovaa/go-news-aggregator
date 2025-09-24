// config предоставляет структуру конфигурации сервиса и функции
// загрузки из файла/переменных окружения с предсказуемым приоритетом.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config — корневая конфигурация сервиса.
// Источники значений (по убыванию приоритета):
//  1. явный путь через флаг --config;
//  2. путь в переменной окружения CONFIG_PATH;
//  3. файл .yaml из рабочей директории;
//  4. переменные окружения (cleanenv).
type Config struct {
	Env      string        `yaml:"env" env:"ENV" env-default:"local"`
	HTTP     HTTPConfig    `yaml:"http"`
	GRPC     GRPCConfig    `yaml:"grpc"`
	Auth     AuthConfig    `yaml:"auth"`
	DB       DBConfig      `yaml:"db"`
	Timeouts TimeoutConfig `yaml:"timeouts"`
}

// TimeoutConfig — таймауты сервиса.
type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env:"SERVICE" env-default:"5s"`
}

// HTTPConfig — сетевые настройки HTTP-сервера.
type HTTPConfig struct {
	Host string `yaml:"host" env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"HTTP_PORT" env-default:"50081"`
}

// GRPCConfig описывает сетевые настройки gRPC-сервера.
type GRPCConfig struct {
	Host string `yaml:"host" env:"HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"PORT" env-default:"50051"`
}

// Addr возвращает адрес в формате host:port.
func (g HTTPConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

// Addr возвращает адрес в формате host:port.
func (g GRPCConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

// AuthConfig содержит параметры выпуска и валидации токенов.
type AuthConfig struct {
	JWTSecret       string        `yaml:"jwt_secret" env:"JWT_SECRET" env-required:"true"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env:"ACCESS_TOKEN_TTL" env-default:"15m"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env:"REFRESH_TOKEN_TTL" env-default:"720h"`
	Issuer          string        `yaml:"issuer"   env:"ISSUER" env-default:"auth-service"`
	Audience        []string      `yaml:"audience" env:"AUDIENCE" env-default:"api-gateway"`
}

// DBConfig — настройки подключения к базе данных.
type DBConfig struct {
	DatabaseURL string `yaml:"db_url" env:"DATABASE_URL" env-required:"true"`
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
// 1. явный путь (аргумент функции, передаётся из main через флаг --config);
// 2. переменная окружения CONFIG_PATH;
// 3. файл ./local.yaml;
// 4. иначе - из переменных окружения.
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
