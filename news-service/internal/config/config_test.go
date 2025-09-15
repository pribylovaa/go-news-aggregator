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
  port: "6000"
db:
  url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
fetcher:
  sources: ["https://a.example/rss.xml", "https://b.example/feed"]
  interval: "11m"
limits:
  default: 15
  max: 200
`

// Минимально валидный YAML (только обязательные поля).
const minimalYAML = `
db:
  url: "postgres://localhost/min"
fetcher:
  sources: ["https://example.org/rss.xml"]
`

// Некорректный YAML — для проверки ошибок парсинга.
const brokenYAML = `
db:
  url: "postgres://broken"
fetcher:
  sources: ["https://example.org/rss.xml"
`

// TestGRPCConfig_Addr — проверяем, что Addr() корректно собирает host:port.
func TestGRPCConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := GRPCConfig{Host: "127.0.0.1", Port: "50051"}
	require.Equal(t, "127.0.0.1:50051", cfg.Addr())
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
	require.Equal(t, "6000", cfg.GRPC.Port)
	require.Equal(t, "postgres://user:pass@localhost:5432/db?sslmode=disable", cfg.DB.URL)
	require.ElementsMatch(t, []string{"https://a.example/rss.xml", "https://b.example/feed"}, cfg.Fetcher.Sources)
	require.Equal(t, 11*time.Minute, cfg.Fetcher.Interval)
	require.EqualValues(t, 15, cfg.LimitsConfig.Default)
	require.EqualValues(t, 200, cfg.LimitsConfig.Max)
}

// TestLoad_WithExplicitPath_FileDoesNotExist — явный путь на несуществующий файл.
func TestLoad_WithExplicitPath_FileDoesNotExist(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := Load(missing)
	require.Error(t, err)
	require.Contains(t, err.Error(), "config file does not exist")
}

// TestLoad_WithExplicitPath_BrokenYAML — битый YAML по явному пути.
func TestLoad_WithExplicitPath_BrokenYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "broken.yaml", brokenYAML)

	_, err := Load(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read config")
}

// TestLoad_WithCONFIG_PATH_OK — путь берётся из CONFIG_PATH.
func TestLoad_WithCONFIG_PATH_OK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "from_env_path.yaml", minimalYAML)
	t.Setenv("CONFIG_PATH", cfgPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "postgres://localhost/min", cfg.DB.URL)
	require.ElementsMatch(t, []string{"https://example.org/rss.xml"}, cfg.Fetcher.Sources)
	// Берутся дефолты для остальных полей.
	require.Equal(t, "local", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.GRPC.Host)
	require.Equal(t, "50051", cfg.GRPC.Port)
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
	require.Equal(t, "postgres://user:pass@localhost:5432/db?sslmode=disable", cfg.DB.URL)
}

// TestLoad_EnvOnly_OK — конфигурация полностью из ENV без YAML-файлов.
func TestLoad_EnvOnly_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	// Минимально необходимые ENV.
	t.Setenv("DATABASE_URL", "postgres://env/db")
	t.Setenv("RSS_SOURCES", "https://a.example/rss.xml,https://b.example/rss.xml")
	t.Setenv("FETCH_INTERVAL", "13m")
	// Необязательные + дефолтные.
	t.Setenv("ENV", "dev")
	t.Setenv("GRPC_HOST", "127.0.0.1")
	t.Setenv("GRPC_PORT", "7001")
	t.Setenv("DEFAULT_LIMIT", "21")
	t.Setenv("MAX_LIMIT", "333")

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "7001", cfg.GRPC.Port)
	require.Equal(t, "postgres://env/db", cfg.DB.URL)
	require.Equal(t, 13*time.Minute, cfg.Fetcher.Interval)
	require.EqualValues(t, 21, cfg.LimitsConfig.Default)
	require.EqualValues(t, 333, cfg.LimitsConfig.Max)
	require.ElementsMatch(t, []string{"https://a.example/rss.xml", "https://b.example/rss.xml"}, cfg.Fetcher.Sources)
}

// TestLoad_Priority_ExplicitWinsOverEnvAndLocal — явный путь важнее CONFIG_PATH и local.yaml.
func TestLoad_Priority_ExplicitWinsOverEnvAndLocal(t *testing.T) {
	dir := t.TempDir()

	explicit := writeFile(t, dir, "explicit.yaml", `
env: "prod"
db: { url: "postgres://explicit/db" }
fetcher: { sources: ["https://explicit/rss.xml"], interval: "10m" }
`)
	badEnvPath := writeFile(t, dir, "env_bad.yaml", brokenYAML)
	t.Setenv("CONFIG_PATH", badEnvPath)
	writeFile(t, dir, "local.yaml", `
env: "local"
db: { url: "postgres://local/db" }
fetcher: { sources: ["https://local/rss.xml"], interval: "10m" }
`)

	chdir(t, dir)

	cfg, err := Load(explicit)
	require.NoError(t, err)

	require.Equal(t, "postgres://explicit/db", cfg.DB.URL)
	require.ElementsMatch(t, []string{"https://explicit/rss.xml"}, cfg.Fetcher.Sources)
}

// TestLoad_Priority_ENVWinsOverLocal — CONFIG_PATH важнее local.yaml.
func TestLoad_Priority_ENVWinsOverLocal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, dir, "local.yaml", `
env: "local"
db: { url: "postgres://local/db" }
fetcher: { sources: ["https://local/rss.xml"], interval: "10m" }
`)
	envPath := writeFile(t, dir, "from_env.yaml", `
env: "dev"
db: { url: "postgres://env/db" }
fetcher: { sources: ["https://env/rss.xml"], interval: "12m" }
`)
	t.Setenv("CONFIG_PATH", envPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "postgres://env/db", cfg.DB.URL)
	require.ElementsMatch(t, []string{"https://env/rss.xml"}, cfg.Fetcher.Sources)
	require.Equal(t, 12*time.Minute, cfg.Fetcher.Interval)
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

// TestMustLoad_OK — успешная загрузка по явному пути.
func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, "postgres://localhost/min", cfg.DB.URL)
	require.ElementsMatch(t, []string{"https://example.org/rss.xml"}, cfg.Fetcher.Sources)
}

// TestMustLoad_PanicsOnError — паника при ошибке загрузки.
func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}
