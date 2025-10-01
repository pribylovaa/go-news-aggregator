package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeFile — утилита записи временного файла конфигурации.
func writeFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(data), 0o600))
	return p
}

// chdir — смена текущего рабочего каталога с авто-возвратом.
func chdir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

// Полный корректный YAML под текущую структуру config.go.
const sampleYAML = `
env: "prod"
http:
  host: "0.0.0.0"
  port: "8080"
metrics:
  host: "127.0.0.1"
  port: "9090"
grpc:
  auth_addr: "10.0.0.1:50081"
  news_addr: "10.0.0.2:50082"
  users_addr: "10.0.0.3:50083"
  comments_addr: "10.0.0.4:50084"
timeouts:
  service: "3s"
`

// Минимальный YAML (всё остальное — через дефолты/ENV).
const minimalYAML = `
env: "stage"
`

// Некорректный YAML для проверки сообщений об ошибке.
const brokenYAML = `
env: [unclosed
`

// --- Адреса HTTP/Metrics (JoinHostPort) ---

func TestHTTPConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := HTTPConfig{Host: "0.0.0.0", Port: "8080"}
	require.Equal(t, "0.0.0.0:8080", cfg.Addr())
}

func TestMetricsConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := MetricsConfig{Host: "127.0.0.1", Port: "9090"}
	require.Equal(t, "127.0.0.1:9090", cfg.Addr())
}

func TestLoad_WithExplicitPath_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "8080", cfg.HTTP.Port)
	require.Equal(t, "127.0.0.1", cfg.Metrics.Host)
	require.Equal(t, "9090", cfg.Metrics.Port)

	require.Equal(t, "10.0.0.1:50081", cfg.GRPC.AuthAddr)
	require.Equal(t, "10.0.0.2:50082", cfg.GRPC.NewsAddr)
	require.Equal(t, "10.0.0.3:50083", cfg.GRPC.UsersAddr)
	require.Equal(t, "10.0.0.4:50084", cfg.GRPC.CommentsAddr)

	require.Equal(t, 3*time.Second, cfg.Timeouts.Service)
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
	require.Equal(t, "stage", cfg.Env)
}

func TestLoad_WithLocalYAML_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, ".", "local.yaml", sampleYAML)
	t.Setenv("CONFIG_PATH", "")

	cfg, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "8080", cfg.HTTP.Port)
}

// CONFIG_PATH важнее local.yaml.
func TestLoad_Priority_ENVWinsOverLocal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, ".", "local.yaml", `
env: "local"
http: { host: "127.0.0.1", port: "7777" }
`)

	envPath := writeFile(t, dir, "from_env.yaml", minimalYAML)
	t.Setenv("CONFIG_PATH", envPath)

	cfg, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "stage", cfg.Env)
}

// Явный путь важнее CONFIG_PATH и local.yaml.
func TestLoad_Priority_ExplicitWinsOverEnvAndLocal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	explicit := writeFile(t, dir, "explicit.yaml", `
env: "prod"
http: { host: "0.0.0.0", port: "8080" }
`)
	badFromEnv := writeFile(t, dir, "bad.yaml", brokenYAML)
	t.Setenv("CONFIG_PATH", badFromEnv)
	writeFile(t, ".", "local.yaml", `
env: "local"
http: { host: "127.0.0.1", port: "9999" }
`)

	cfg, err := Load(explicit)
	require.NoError(t, err)
	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "8080", cfg.HTTP.Port)
}

func TestLoad_EnvOverlay_OverridesValuesFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	// Меняем некоторые поля через ENV.
	t.Setenv("HTTP_PORT", "18080")
	t.Setenv("METRICS_HOST", "0.0.0.0")
	t.Setenv("GRPC_AUTH_ADDR", "1.2.3.4:60081")
	t.Setenv("SERVICE", "5s") // таймаут

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	require.Equal(t, "18080", cfg.HTTP.Port)
	require.Equal(t, "0.0.0.0", cfg.Metrics.Host)
	require.Equal(t, "1.2.3.4:60081", cfg.GRPC.AuthAddr)
	require.Equal(t, 5*time.Second, cfg.Timeouts.Service)
}

// «Только ENV» без файлов.
func TestLoad_EnvOnly_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	t.Setenv("ENV", "dev")
	t.Setenv("HTTP_HOST", "0.0.0.0")
	t.Setenv("HTTP_PORT", "50090")
	t.Setenv("METRICS_HOST", "127.0.0.1")
	t.Setenv("METRICS_PORT", "50085")
	t.Setenv("GRPC_AUTH_ADDR", "a:1")
	t.Setenv("GRPC_NEWS_ADDR", "b:2")
	t.Setenv("GRPC_USERS_ADDR", "c:3")
	t.Setenv("GRPC_COMMENTS_ADDR", "d:4")
	t.Setenv("SERVICE", "2s")

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "50090", cfg.HTTP.Port)
	require.Equal(t, "127.0.0.1", cfg.Metrics.Host)
	require.Equal(t, "50085", cfg.Metrics.Port)
	require.Equal(t, "a:1", cfg.GRPC.AuthAddr)
	require.Equal(t, "b:2", cfg.GRPC.NewsAddr)
	require.Equal(t, "c:3", cfg.GRPC.UsersAddr)
	require.Equal(t, "d:4", cfg.GRPC.CommentsAddr)
	require.Equal(t, 2*time.Second, cfg.Timeouts.Service)
}

func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, "stage", cfg.Env)
}

func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}
