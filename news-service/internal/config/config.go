// config предоставляет структуру конфигурации news-service
// и функции загрузки из YAML/ENV с предсказуемым приоритетом.
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
	Env          string        `yaml:"env"     env:"ENV"        env-default:"local"`
	HTTP         HTTPConfig    `yaml:"http"`
	GRPC         GRPCConfig    `yaml:"grpc"`
	DB           DBConfig      `yaml:"db"`
	Fetcher      FetcherConfig `yaml:"fetcher"`
	LimitsConfig LimitsConfig  `yaml:"limits"`
	Timeouts     TimeoutConfig `yaml:"timeouts"`
}

// TimeoutConfig — таймауты сервиса.
type TimeoutConfig struct {
	Service time.Duration `yaml:"service" env:"SERVICE" env-default:"5s"`
}

// GRPCConfig — сетевые настройки gRPC-сервера.
type GRPCConfig struct {
	Host string `yaml:"host" env:"GRPC_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"GRPC_PORT" env-default:"50052"`
}

// HTTPConfig — сетевые настройки HTTP-сервера.
type HTTPConfig struct {
	Host string `yaml:"host" env:"HTTP_HOST" env-default:"0.0.0.0"`
	Port string `yaml:"port" env:"HTTP_PORT" env-default:"50082"`
}

// Addr возвращает адрес в формате host:port.
func (g GRPCConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

// Addr возвращает адрес в формате host:port.
func (g HTTPConfig) Addr() string {
	return net.JoinHostPort(g.Host, g.Port)
}

// DBConfig — настройки подключения к базе данных.
type DBConfig struct {
	URL string `yaml:"url" env:"DATABASE_URL" env-required:"true"`
}

// FetcherConfig — параметры периодического опроса RSS.
type FetcherConfig struct {
	// Список URL RSS-источников. Можно задать через ENV RSS_SOURCES, разделитель — запятая.
	Sources  []string      `yaml:"sources"  env:"RSS_SOURCES"   env-separator:","`
	Interval time.Duration `yaml:"interval" env:"FETCH_INTERVAL" env-default:"10m"`
}

// LimitsConfig — серверные лимиты на выдачу.
type LimitsConfig struct {
	// Применяется при запросе с limit=0.
	Default int32 `yaml:"default" env:"DEFAULT_LIMIT" env-default:"12"`
	// Верхняя граница для limit.
	Max int32 `yaml:"max" env:"MAX_LIMIT" env-default:"300"`
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
			return nil, fmt.Errorf("config file does not exist: %s", p)
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

// validate — базовая валидация значений.
func (c *Config) validate() error {
	if c.DB.URL == "" {
		return fmt.Errorf("db.url is required")
	}
	if len(c.Fetcher.Sources) == 0 {
		return fmt.Errorf("fetcher.sources must contain at least one RSS feed")
	}
	if c.Fetcher.Interval < time.Minute {
		return fmt.Errorf("fetcher.interval must be at least 1m")
	}
	if c.LimitsConfig.Default <= 0 {
		return fmt.Errorf("limits.default must be > 0")
	}
	if c.LimitsConfig.Max <= 0 {
		return fmt.Errorf("limits.max must be > 0")
	}
	if c.LimitsConfig.Default > c.LimitsConfig.Max {
		return fmt.Errorf("limits.default must be <= limits.max")
	}
	return nil
}
