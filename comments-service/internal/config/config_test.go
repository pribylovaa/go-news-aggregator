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
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	return path
}

// chdir — смена текущего рабочего каталога с автоматическим откатом.
func chdir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

// Полный корректный YAML (не зависит от дефолтов).
const sampleYAML = `
env: "prod"
grpc:
  host: "127.0.0.1"
  port: "6001"
http:
  host: "0.0.0.0"
  port: "8081"
db:
  url: "mongodb://user:pass@localhost:27017/comments?replicaSet=rs0"
limits:
  default: 15
  max: 200
  max_depth: 8
ttl:
  thread: "240h"
timeouts:
  service: 3s
`

// Минимально валидный YAML (только обязательные поля).
const minimalYAML = `
db:
  url: "mongodb://localhost:27017/comments"
`

// Некорректный YAML — для проверки ошибок парсинга.
const brokenYAML = `
db:
  url: "mongodb://broken"
limits:
  default: 10
  max: 5
ttl:
  thread: "240h"
timeouts:
  service: 3s
# пропущена закрывающая скобка у массива (разрыв синтаксиса)
http:
  host: "0.0.0.0"
  port: "8081"
grpc:
  host: "127.0.0.1"
  port: "6001"
`

// TestGRPCConfig_Addr — проверяем, что GRPC.Addr() корректно собирает host:port.
func TestGRPCConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := GRPCConfig{Host: "127.0.0.1", Port: "50054"}
	require.Equal(t, "127.0.0.1:50054", cfg.Addr())
}

// TestHTTPConfig_Addr — проверяем, что HTTP.Addr() корректно собирает host:port.
func TestHTTPConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := HTTPConfig{Host: "0.0.0.0", Port: "50084"}
	require.Equal(t, "0.0.0.0:50084", cfg.Addr())
}

// TestLoad_WithExplicitPath_OK — явный путь имеет высший приоритет.
func TestLoad_WithExplicitPath_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "6001", cfg.GRPC.Port)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "8081", cfg.HTTP.Port)
	require.Equal(t, "mongodb://user:pass@localhost:27017/comments?replicaSet=rs0", cfg.DB.URL)

	require.EqualValues(t, int32(15), cfg.Limits.Default)
	require.EqualValues(t, int32(200), cfg.Limits.Max)
	require.EqualValues(t, int32(8), cfg.Limits.MaxDepth)

	require.Equal(t, 240*time.Hour, cfg.TTL.Thread)
	require.Equal(t, 3*time.Second, cfg.Timeouts.Service)
}

// TestLoad_WithExplicitPath_BrokenYAML — битый YAML по явному пути.
func TestLoad_WithExplicitPath_BrokenYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "broken.yaml", brokenYAML)

	_, err := Load(cfgPath)
	require.Error(t, err)
}

// TestLoad_WithCONFIG_PATH_OK — путь берётся из CONFIG_PATH.
func TestLoad_WithCONFIG_PATH_OK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "from_env_path.yaml", minimalYAML)
	t.Setenv("CONFIG_PATH", cfgPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "mongodb://localhost:27017/comments", cfg.DB.URL)

	// Берутся дефолты для остальных полей.
	require.Equal(t, "local", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.GRPC.Host)
	require.Equal(t, "50054", cfg.GRPC.Port)
	require.Equal(t, "0.0.0.0", cfg.HTTP.Host)
	require.Equal(t, "50084", cfg.HTTP.Port)
	require.EqualValues(t, int32(20), cfg.Limits.Default)
	require.EqualValues(t, int32(300), cfg.Limits.Max)
	require.EqualValues(t, int32(6), cfg.Limits.MaxDepth)
	require.Equal(t, 168*time.Hour, cfg.TTL.Thread)
	require.Equal(t, 5*time.Second, cfg.Timeouts.Service)
}

// TestLoad_WithLocalYAML_OK — если нет CONFIG_PATH, берётся ./local.yaml.
func TestLoad_WithLocalYAML_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, ".", "local.yaml", sampleYAML)
	t.Setenv("CONFIG_PATH", "")

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "mongodb://user:pass@localhost:27017/comments?replicaSet=rs0", cfg.DB.URL)
	require.Equal(t, 240*time.Hour, cfg.TTL.Thread)
	require.EqualValues(t, int32(8), cfg.Limits.MaxDepth)
}

// TestLoad_EnvOnly_OK — конфигурация полностью из ENV без YAML-файлов.
func TestLoad_EnvOnly_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	// Минимально необходимые ENV.
	t.Setenv("DATABASE_URL", "mongodb://env/comments")
	// Необязательные + дефолтные.
	t.Setenv("ENV", "dev")
	t.Setenv("GRPC_HOST", "127.0.0.1")
	t.Setenv("GRPC_PORT", "7001")
	t.Setenv("HTTP_HOST", "127.0.0.1")
	t.Setenv("HTTP_PORT", "7081")

	t.Setenv("DEFAULT_LIMIT", "21")
	t.Setenv("MAX_LIMIT", "333")
	t.Setenv("MAX_DEPTH", "9")
	t.Setenv("THREAD_TTL", "200h")
	t.Setenv("SERVICE", "7s")

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "7001", cfg.GRPC.Port)
	require.Equal(t, "127.0.0.1", cfg.HTTP.Host)
	require.Equal(t, "7081", cfg.HTTP.Port)
	require.Equal(t, "mongodb://env/comments", cfg.DB.URL)

	require.EqualValues(t, int32(21), cfg.Limits.Default)
	require.EqualValues(t, int32(333), cfg.Limits.Max)
	require.EqualValues(t, int32(9), cfg.Limits.MaxDepth)
	require.Equal(t, 200*time.Hour, cfg.TTL.Thread)
	require.Equal(t, 7*time.Second, cfg.Timeouts.Service)
}

// TestLoad_Priority_ExplicitWinsOverEnvAndLocal — явный путь важнее CONFIG_PATH и local.yaml.
func TestLoad_Priority_ExplicitWinsOverEnvAndLocal(t *testing.T) {
	dir := t.TempDir()

	explicit := writeFile(t, dir, "explicit.yaml", `
env: "prod"
db: { url: "mongodb://explicit/comments" }
limits: { default: 10, max: 100, max_depth: 5 }
ttl: { thread: "180h" }
`)
	badEnvPath := writeFile(t, dir, "env_bad.yaml", brokenYAML)
	t.Setenv("CONFIG_PATH", badEnvPath)
	writeFile(t, dir, "local.yaml", `
env: "local"
db: { url: "mongodb://local/comments" }
limits: { default: 11, max: 110, max_depth: 6 }
ttl: { thread: "168h" }
`)

	chdir(t, dir)

	cfg, err := Load(explicit)
	require.NoError(t, err)

	require.Equal(t, "mongodb://explicit/comments", cfg.DB.URL)
	require.EqualValues(t, int32(10), cfg.Limits.Default)
	require.Equal(t, 180*time.Hour, cfg.TTL.Thread)
}

// TestLoad_Priority_ENVWinsOverLocal — CONFIG_PATH важнее local.yaml.
func TestLoad_Priority_ENVWinsOverLocal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, dir, "local.yaml", `
env: "local"
db: { url: "mongodb://local/comments" }
limits: { default: 11, max: 110, max_depth: 6 }
ttl: { thread: "168h" }
`)
	envPath := writeFile(t, dir, "from_env.yaml", `
env: "dev"
db: { url: "mongodb://env/comments" }
limits: { default: 12, max: 120, max_depth: 7 }
ttl: { thread: "120h" }
`)
	t.Setenv("CONFIG_PATH", envPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "mongodb://env/comments", cfg.DB.URL)
	require.EqualValues(t, int32(12), cfg.Limits.Default)
	require.EqualValues(t, int32(120), cfg.Limits.Max)
	require.EqualValues(t, int32(7), cfg.Limits.MaxDepth)
	require.Equal(t, 120*time.Hour, cfg.TTL.Thread)
}

// TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError —
// нет ни файлов, ни обязательных ENV -> осмысленная ошибка.
func TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	_, err := Load("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "config not found: provide --config, CONFIG_PATH, local.yaml or env vars")
}

// Доп. негативные проверки валидации под специфику comments-service.

func TestLoad_InvalidTTLThread_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_ttl.yaml", `
db: { url: "mongodb://localhost:27017/comments" }
ttl: { thread: "10m" } # меньше 1h
`)

	_, err := Load(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ttl.thread must be at least 1h")
}

func TestLoad_InvalidLimits_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_limits.yaml", `
db: { url: "mongodb://localhost:27017/comments" }
limits: { default: 100, max: 10, max_depth: 0 }
`)

	_, err := Load(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits.default must be <= limits.max")
}

// TestMustLoad_OK — успешная загрузка по явному пути.
func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, "mongodb://localhost:27017/comments", cfg.DB.URL)
}

// TestMustLoad_PanicsOnError — паника при ошибке загрузки.
func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}
