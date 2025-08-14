package config

import (
	"flag"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env  string     `yaml:"env" env-default:"local"`
	GRPC GRPCConfig `yaml:"grpc"`
	Auth AuthConfig `yaml:"auth"`
	DB   DBConfig   `yaml:"db"`
}

type GRPCConfig struct {
	Host string `yaml:"host" env-default:"0.0.0.0"`
	Port string `yaml:"port" env-default:"50051"`
}

type AuthConfig struct {
	JWTSecret       string        `yaml:"jwt_secret" env-required:"true"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env-default:"15m"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env-default:"720h"`
}

type DBConfig struct {
	DatabaseURL string `yaml:"db_url" env-required:"true"`
}

// MustLoad загружает конфигурацию из файла YAML, путь к которому определяется из флага --config
// или переменной окружения CONFIG_PATH.
func MustLoad() *Config {
	path := fetchConfigPath()
	if path == "" {
		panic("config path is empty")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		panic("config file does not exist: " + path)
	}

	var cfg Config
	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		panic("failed to read config: " + err.Error())
	}

	return &cfg
}

// fetchConfigPath возвращает путь к файлу конфигурации,
// приоритетно используя флаг командной строки --config, затем переменную окружения CONFIG_PATH.
func fetchConfigPath() string {
	var res string
	flag.StringVar(&res, "config", "", "path to config file")
	flag.Parse()

	if res == "" {
		res = os.Getenv("CONFIG_PATH")
	}

	return res
}
