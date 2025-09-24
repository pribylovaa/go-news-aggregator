// config реализует конфигурацию comments-service: загрузка из YAML/ENV с предсказуемым приоритетом.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config — корневая конфигурация сервиса.
// Приоритет источников:
//  1. явный путь, переданный в MustLoad/Load;
//  2. переменная окружения CONFIG_PATH;
//  3. файл ./local.yaml из рабочей директории;
//  4. переменные окружения.
type Config struct {
	Env      string        `yaml:"env" env:"ENV" env-default:"local"`
	GRPC     GRPCConfig    `yaml:"grpc"`
	HTTP     HTTPConfig    `yaml:"http"`
	DB       DBConfig      `yaml:"db"`
	Limits   LimitsConfig  `yaml:"limits"`
	TTL      TTLConfig     `yaml:"ttl"`
	Timeouts TimeoutConfig `yaml:"timeouts"`
}

// TimeoutConfig — сервисные таймауты (общий дедлайн обработки запроса).
type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env:"SERVICE" env-default:"5s"`
}

// GRPCConfig — сетевые настройки gRPC-сервера.
type GRPCConfig struct {
	Host string `yaml:"host" env:"GRPC_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"GRPC_PORT" env-default:"50054"`
}

// HTTPConfig — опциональный HTTP (health/metrics/pprof).
type HTTPConfig struct {
	Host string `yaml:"host" env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"HTTP_PORT" env-default:"50084"`
}

// Addr возвращает адрес в формате host:port.
func (g GRPCConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

// Addr возвращает адрес в формате host:port.
func (h HTTPConfig) Addr() string {
	return net.JoinHostPort(h.Host, h.Port)
}

// DBConfig — настройки подключения к MongoDB.
type DBConfig struct {
	URL string `yaml:"url" env:"DATABASE_URL" env-required:"true"`
}

// TTLConfig — управление временем жизни веток комментариев.
type TTLConfig struct {
	// Срок жизни корневой ветки; ответы получают тот же expires_at, что и корень.
	// Значение должно быть >= 1h. По умолчанию 7 дней.
	Thread time.Duration `yaml:"thread" env:"THREAD_TTL" env-default:"168h"`
}

// LimitsConfig — лимиты на выдачу и глубину дерева.
type LimitsConfig struct {
	// Пагинация: page_size=0 -> берём Default; верхняя граница — Max.
	Default int32 `yaml:"default"   env:"DEFAULT_LIMIT" env-default:"20"`
	Max     int32 `yaml:"max"       env:"MAX_LIMIT"     env-default:"300"`
	// Максимально допустимая глубина ветвления (level). Корень = 0.
	MaxDepth int32 `yaml:"max_depth" env:"MAX_DEPTH"    env-default:"6"`
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
// После чтения файла накладываем ENV-переменные поверх значений из YAML.
func Load(path string) (*Config, error) {
	var cfg Config

	// чтение файла + overlay ENV.
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

		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("failed to overlay env: %w", err)
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

// validate — базовая валидация значений.
func (c *Config) validate() error {
	if c.DB.URL == "" {
		return fmt.Errorf("db.url is required")
	}

	if c.TTL.Thread < time.Hour {
		return fmt.Errorf("ttl.thread must be at least 1h")
	}

	if c.Limits.Default <= 0 {
		return fmt.Errorf("limits.default must be > 0")
	}

	if c.Limits.Max <= 0 {
		return fmt.Errorf("limits.max must be > 0")
	}

	if c.Limits.Default > c.Limits.Max {
		return fmt.Errorf("limits.default must be <= limits.max")
	}

	if c.Limits.MaxDepth <= 0 {
		return fmt.Errorf("limits.max_depth must be > 0")
	}

	if c.Limits.MaxDepth > 32 {
		return fmt.Errorf("limits.max_depth is too large (<= 32)")
	}

	return nil
}
