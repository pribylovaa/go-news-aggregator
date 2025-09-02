package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Файл тестов на пакет config:
// - проверяет приоритет источников (явный путь > CONFIG_PATH > local.yaml > ENV);
// - валидирует корректность парсинга YAML/ENV и юнитов длительностей;
// - проверяет вспомогательное форматирование адреса в GRPCConfig.Addr().

// writeFile — утилита записи временного файла конфигурации.
func writeFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	return path
}

// chdir — смена текущего рабочего каталога
// с автоматическим возвратом по окончании теста.
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

// TestGRPCConfig_Addr — проверяем, что Addr() использует
// net.JoinHostPort (корректная сборка host:port).
func TestGRPCConfig_Addr(t *testing.T) {
	t.Parallel()
	// фиксируем входные значения хоста и порта.
	cfg := GRPCConfig{Host: "127.0.0.1", Port: "50051"}
	// ожидаем корректный формат адреса.
	require.Equal(t, "127.0.0.1:50051", cfg.Addr())
}

// TestLoad_WithExplicitPath_OK — явный путь имеет высший приоритет.
func TestLoad_WithExplicitPath_OK(t *testing.T) {
	t.Parallel()

	// кладём валидный YAML во временной директории.
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	// грузим конфиг строго по явному пути (высший приоритет).
	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// сверяем все ключевые поля и unit-ы длительностей.
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

// TestLoad_WithExplicitPath_FileDoesNotExist — явный путь на несуществующий файл.
func TestLoad_WithExplicitPath_FileDoesNotExist(t *testing.T) {
	t.Parallel()

	// формируем путь к отсутствующему файлу в temp-директории.
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	// ожидаем диагностическую ошибку про отсутствие файла.
	_, err := Load(missing)
	require.Error(t, err)
	require.Contains(t, err.Error(), "config file does not exist")
}

// TestLoad_WithExplicitPath_BrokenYAML — битый YAML по явному пути
// должен привести к ошибке чтения.
func TestLoad_WithExplicitPath_BrokenYAML(t *testing.T) {
	t.Parallel()

	// пишем некорректный YAML.
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "broken.yaml", brokenYAML)

	// ожидаем ошибку парсинга с понятным сообщением.
	_, err := Load(cfgPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read config")
}

// TestLoad_WithCONFIG_PATH_OK — путь приходит из CONFIG_PATH и используется для загрузки.
func TestLoad_WithCONFIG_PATH_OK(t *testing.T) {
	// готовим минимальный корректный YAML и канал к нему в CONFIG_PATH.
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "from_env_path.yaml", minimalYAML)
	t.Setenv("CONFIG_PATH", cfgPath)

	// загружаем без явного пути — должен сработать CONFIG_PATH.
	cfg, err := Load("")
	require.NoError(t, err)

	// проверяем обязательные параметры.
	require.Equal(t, "min-secret", cfg.Auth.JWTSecret)
	require.Equal(t, "postgres://localhost/min", cfg.DB.DatabaseURL)
}

// TestLoad_WithLocalYAML_OK — при пустом CONFIG_PATH берётся ./local.yaml из CWD.
func TestLoad_WithLocalYAML_OK(t *testing.T) {
	// изолируем рабочую директорию и кладём в неё local.yaml.
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, ".", "local.yaml", sampleYAML)
	t.Setenv("CONFIG_PATH", "")

	// загружаем — должен подтянуться local.yaml.
	cfg, err := Load("")
	require.NoError(t, err)

	// базовые проверки значений из local.yaml.
	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "super-secret", cfg.Auth.JWTSecret)
}

// TestLoad_EnvOnly_OK — конфигурация полностью из ENV без YAML-файлов.
func TestLoad_EnvOnly_OK(t *testing.T) {
	// обнуляем влияние local.yaml / CONFIG_PATH.
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	// задаём все необходимые ENV-переменные.
	t.Setenv("JWT_SECRET", "env-secret")
	t.Setenv("DATABASE_URL", "postgres://env/db")
	t.Setenv("ENV", "dev")
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "12345")
	t.Setenv("ACCESS_TOKEN_TTL", "1m")
	t.Setenv("REFRESH_TOKEN_TTL", "100h")
	t.Setenv("ISSUER", "issuer-env")
	t.Setenv("SERVICE", "2s")

	// загружаем — должен сработать путь «только ENV».
	cfg, err := Load("")
	require.NoError(t, err)

	// проверяем, что значения пришли именно из ENV.
	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.GRPC.Host)
	require.Equal(t, "12345", cfg.GRPC.Port)
	require.Equal(t, "env-secret", cfg.Auth.JWTSecret)
	require.Equal(t, time.Minute, cfg.Auth.AccessTokenTTL)
	require.Equal(t, 100*time.Hour, cfg.Auth.RefreshTokenTTL)
	require.Equal(t, "issuer-env", cfg.Auth.Issuer)
	require.Equal(t, "postgres://env/db", cfg.DB.DatabaseURL)
	require.Equal(t, 2*time.Second, cfg.Timeouts.Service)
}

// TestLoad_Priority_ExplicitWinsOverEnvAndLocal — явный путь важнее CONFIG_PATH и local.yaml.
func TestLoad_Priority_ExplicitWinsOverEnvAndLocal(t *testing.T) {
	// готовим три источника: explicit.yaml, битый CONFIG_PATH, корректный local.yaml.
	dir := t.TempDir()

	explicit := writeFile(t, dir, "explicit.yaml", `
env: "prod"
db: { db_url: "postgres://explicit/db" }
auth: { jwt_secret: "explicit-secret" }
`)
	badEnvPath := writeFile(t, dir, "env_bad.yaml", brokenYAML)
	t.Setenv("CONFIG_PATH", badEnvPath)
	writeFile(t, dir, "local.yaml", `
env: "local"
db: { db_url: "postgres://local/db" }
auth: { jwt_secret: "local-secret" }
`)
	chdir(t, dir)

	// загружаем по явному пути — должны игнорироваться CONFIG_PATH и local.yaml.
	cfg, err := Load(explicit)
	require.NoError(t, err)

	// проверяем значения из explicit.yaml.
	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "postgres://explicit/db", cfg.DB.DatabaseURL)
	require.Equal(t, "explicit-secret", cfg.Auth.JWTSecret)
}

// TestLoad_Priority_ENVWinsOverLocal — CONFIG_PATH важнее local.yaml.
func TestLoad_Priority_ENVWinsOverLocal(t *testing.T) {
	// в CWD есть local.yaml, но в CONFIG_PATH задаем иной путь.
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, dir, "local.yaml", `
env: "local"
db: { db_url: "postgres://local/db" }
auth: { jwt_secret: "local-secret" }
`)
	envPath := writeFile(t, dir, "from_env.yaml", `
env: "dev"
db: { db_url: "postgres://env/db" }
auth: { jwt_secret: "env-secret" }
`)
	t.Setenv("CONFIG_PATH", envPath)

	// загружаем — ожидаем значения именно из файла, указанного в CONFIG_PATH.
	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "postgres://env/db", cfg.DB.DatabaseURL)
	require.Equal(t, "env-secret", cfg.Auth.JWTSecret)
}

// TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError —
// нет ни файлов, ни обязательных ENV -> осмысленная ошибка.
func TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError(t *testing.T) {
	// убираем влияние файлов и CONFIG_PATH.
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	// пробуем загрузить «пустой» конфиг — ожидаем полезное диагностическое сообщение.
	_, err := Load("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "config not found: provide --config, CONFIG_PATH, local.yaml or env vars")
}

// TestMustLoad_OK — успешная загрузка по явному пути.
func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	// кладём минимальный валидный YAML и грузим через MustLoad.
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)

	// проверяем ключевые поля.
	require.NotNil(t, cfg)
	require.Equal(t, "min-secret", cfg.Auth.JWTSecret)
	require.Equal(t, "postgres://localhost/min", cfg.DB.DatabaseURL)
}

// TestMustLoad_PanicsOnError — паника при ошибке загрузки.
func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	// указываем несуществующий путь и ожидаем панику.
	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}
