package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Вспомогательные хелперы.
func writeFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	return path
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

// Полный корректный YAML с заданными значениями (не зависящими от дефолтов).
const sampleYAML = `
env: "prod"
grpc:
  host: "127.0.0.1"
  port: "6000"
auth:
  jwt_secret: "super-secret"
  access_token_ttl: "10m"
  refresh_token_ttl: "240h"
  issuer: "issuerX"
  audience: ["api-gateway", "web"]
db:
  db_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
timeouts:
  service: "3s"
`

// Минимально валидный YAML (только обязательные поля).
const minimalYAML = `
auth:
  jwt_secret: "min-secret"
db:
  db_url: "postgres://localhost/min"
`

// Некорректный YAML — для проверки ошибок парсинга.
const brokenYAML = `
auth:
  jwt_secret: [unclosed
`

func TestLoad_WithExplicitPath_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "6000", cfg.GRPC.Port)

	require.Equal(t, "super-secret", cfg.Auth.JWTSecret)
	require.Equal(t, 10*time.Minute, cfg.Auth.AccessTokenTTL)
	require.Equal(t, 240*time.Hour, cfg.Auth.RefreshTokenTTL)
	require.Equal(t, "issuerX", cfg.Auth.Issuer)
	require.ElementsMatch(t, []string{"api-gateway", "web"}, cfg.Auth.Audience)

	require.Equal(t, "postgres://user:pass@localhost:5432/db?sslmode=disable", cfg.DB.DatabaseURL)
	require.Equal(t, 3*time.Second, cfg.Timeouts.Service)
}

func TestLoad_WithExplicitPath_FileDoesNotExist(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "config file does not exist")
}

func TestLoad_WithExplicitPath_BrokenYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "broken.yaml", brokenYAML)

	_, err := Load(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read config")
}

func TestLoad_WithCONFIG_PATH_OK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "from_env_path.yaml", minimalYAML)

	t.Setenv("CONFIG_PATH", cfgPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "min-secret", cfg.Auth.JWTSecret)
	require.Equal(t, "postgres://localhost/min", cfg.DB.DatabaseURL)
}

func TestLoad_WithLocalYAML_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, ".", "local.yaml", sampleYAML)

	t.Setenv("CONFIG_PATH", "")
	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "super-secret", cfg.Auth.JWTSecret)
}

func TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	_, err := Load("")
	require.Error(t, err)

	require.Contains(t, err.Error(), "config not found: provide --config, CONFIG_PATH, local.yaml or env vars")
}

func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, "min-secret", cfg.Auth.JWTSecret)
	require.Equal(t, "postgres://localhost/min", cfg.DB.DatabaseURL)
}

func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}
