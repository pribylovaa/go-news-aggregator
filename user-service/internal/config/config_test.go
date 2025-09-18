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
postgres:
  url: "postgres://user:pass@localhost:5432/userdb?sslmode=disable"
s3:
  endpoint: "http://minio:9000"
  root_user: "root"
  root_password: "rootpass"
  bucket: "avatars"
  presign_ttl: "17m"
avatar:
  max_size_bytes: 1048576
  allowed_content_types: ["image/jpeg", "image/webp"]
timeouts:
  service: 7s
`

// Минимально валидный YAML: только обязательные поля, остальное — через env-default.
const minimalYAML = `
postgres:
  url: "postgres://localhost/user-min"
s3:
  endpoint: "http://minio:9000"
  root_user: "root"
  root_password: "rootpass"
  bucket: "avatars"
`

// Некорректный YAML — для проверки ошибок парсинга.
const brokenYAML = `
postgres:
  url: "postgres://broken"
s3:
  endpoint: "http://minio:9000"
  root_user: "root"
  root_password: "rootpass"
  bucket: "avatars"
avatar:
  allowed_content_types: ["image/jpeg"
  max_size_bytes: -6
`

func TestGRPCConfig_Addr(t *testing.T) {
	t.Parallel()
	cfg := GRPCConfig{Host: "127.0.0.1", Port: "50051"}
	require.Equal(t, "127.0.0.1:50051", cfg.Addr())
}

func TestLoad_WithExplicitPath_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "config.yaml", sampleYAML)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "6001", cfg.GRPC.Port)

	require.Equal(t, "postgres://user:pass@localhost:5432/userdb?sslmode=disable", cfg.Postgres.URL)

	require.Equal(t, "http://minio:9000", cfg.S3.Endpoint)
	require.Equal(t, "root", cfg.S3.RootUser)
	require.Equal(t, "rootpass", cfg.S3.RootPassword)
	require.Equal(t, "avatars", cfg.S3.Bucket)
	require.Equal(t, 17*time.Minute, cfg.S3.PresignTTL)

	require.EqualValues(t, int64(1048576), cfg.Avatar.MaxSizeBytes)
	require.ElementsMatch(t, []string{"image/jpeg", "image/webp"}, cfg.Avatar.AllowedContentTypes)

	require.EqualValues(t, 7*time.Second, cfg.Timeouts.Service)
}

func TestLoad_WithExplicitPath_FileDoesNotExist(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := Load(missing)
	require.Error(t, err)
}

func TestLoad_WithExplicitPath_BrokenYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "broken.yaml", brokenYAML)

	_, err := Load(cfgPath)
	require.Error(t, err)
}

func TestLoad_WithCONFIG_PATH_OK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "from_env_path.yaml", minimalYAML)
	t.Setenv("CONFIG_PATH", cfgPath)

	cfg, err := Load("")
	require.NoError(t, err)

	// Значения по умолчанию из env-default.
	require.Equal(t, "local", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.GRPC.Host)
	require.Equal(t, "50053", cfg.GRPC.Port)

	require.Equal(t, "postgres://localhost/user-min", cfg.Postgres.URL)

	require.Equal(t, "http://minio:9000", cfg.S3.Endpoint)
	require.Equal(t, "root", cfg.S3.RootUser)
	require.Equal(t, "rootpass", cfg.S3.RootPassword)
	require.Equal(t, "avatars", cfg.S3.Bucket)
	require.Equal(t, 10*time.Minute, cfg.S3.PresignTTL)

	require.EqualValues(t, int64(5*1024*1024), cfg.Avatar.MaxSizeBytes)
	require.ElementsMatch(t, []string{"image/jpeg", "image/png"}, cfg.Avatar.AllowedContentTypes)

	require.EqualValues(t, 5*time.Second, cfg.Timeouts.Service)
}

func TestLoad_WithLocalYAML_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, ".", "local.yaml", sampleYAML)
	t.Setenv("CONFIG_PATH", "")

	cfg, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "prod", cfg.Env)
	require.Equal(t, "postgres://user:pass@localhost:5432/userdb?sslmode=disable", cfg.Postgres.URL)
}

func TestLoad_EnvOnly_OK(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	// Минимально необходимые ENV.
	t.Setenv("POSTGRES", "postgres://env/userdb")
	t.Setenv("S3_ENDPOINT", "http://127.0.0.1:9000")
	t.Setenv("S3_ROOT_USER", "u")
	t.Setenv("S3_ROOT_PASSWORD", "p")
	t.Setenv("S3_BUCKET", "bkt")
	// Необязательные + дефолтные.
	t.Setenv("ENV", "dev")
	t.Setenv("GRPC_HOST", "127.0.0.1")
	t.Setenv("GRPC_PORT", "7001")
	t.Setenv("S3_PRESIGN_TTL", "13m")
	t.Setenv("AVATAR_MAX_SIZE_BYTES", "2097152")
	t.Setenv("AVATAR_ALLOWED_CONTENT_TYPES", "image/jpeg,image/svg+xml")
	t.Setenv("SERVICE_TIMEOUT", "4s")

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "127.0.0.1", cfg.GRPC.Host)
	require.Equal(t, "7001", cfg.GRPC.Port)

	require.Equal(t, "postgres://env/userdb", cfg.Postgres.URL)

	require.Equal(t, "http://127.0.0.1:9000", cfg.S3.Endpoint)
	require.Equal(t, "u", cfg.S3.RootUser)
	require.Equal(t, "p", cfg.S3.RootPassword)
	require.Equal(t, "bkt", cfg.S3.Bucket)
	require.Equal(t, 13*time.Minute, cfg.S3.PresignTTL)

	require.EqualValues(t, int64(2097152), cfg.Avatar.MaxSizeBytes)
	require.ElementsMatch(t, []string{"image/jpeg", "image/svg+xml"}, cfg.Avatar.AllowedContentTypes)

	require.EqualValues(t, 4*time.Second, cfg.Timeouts.Service)
}

func TestLoad_Priority_ExplicitWinsOverEnvAndLocal(t *testing.T) {
	dir := t.TempDir()

	explicit := writeFile(t, dir, "explicit.yaml", `
env: "prod"
grpc: { host: "127.0.0.1", port: "6009" }
postgres: { url: "postgres://explicit/db" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
`)
	badEnvPath := writeFile(t, dir, "env_bad.yaml", brokenYAML)
	t.Setenv("CONFIG_PATH", badEnvPath)

	writeFile(t, dir, "local.yaml", `
env: "local"
postgres: { url: "postgres://local/db" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
`)

	chdir(t, dir)

	cfg, err := Load(explicit)
	require.NoError(t, err)

	require.Equal(t, "postgres://explicit/db", cfg.Postgres.URL)
	require.Equal(t, "6009", cfg.GRPC.Port)
}

func TestLoad_Priority_ENVWinsOverLocal(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, dir, "local.yaml", `
env: "local"
postgres: { url: "postgres://local/db" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
`)
	envPath := writeFile(t, dir, "from_env.yaml", `
env: "dev"
postgres: { url: "postgres://env/db" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars", presign_ttl: "12m" }
`)
	t.Setenv("CONFIG_PATH", envPath)

	cfg, err := Load("")
	require.NoError(t, err)

	require.Equal(t, "dev", cfg.Env)
	require.Equal(t, "postgres://env/db", cfg.Postgres.URL)
	require.Equal(t, 12*time.Minute, cfg.S3.PresignTTL)
}

func TestLoad_EnvOnly_NoConfigInEnv_ReturnsDescriptiveError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("CONFIG_PATH", "")

	_, err := Load("")
	require.Error(t, err)
}

func TestLoad_InvalidPort_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_port.yaml", `
grpc: { host: "0.0.0.0", port: "70000" }
postgres: { url: "postgres://x" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
`)
	_, err := Load(cfgPath)
	require.Error(t, err)
}

func TestLoad_NegativePresignTTL_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_ttl.yaml", `
postgres: { url: "postgres://x" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars", presign_ttl: "-6s" }
`)
	_, err := Load(cfgPath)
	require.Error(t, err)
}

func TestLoad_EmptyAllowedContentTypes_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_avatar.yaml", `
postgres: { url: "postgres://x" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
avatar: { allowed_content_types: [] }
`)
	_, err := Load(cfgPath)
	require.Error(t, err)
}

func TestLoad_NegativeMaxAvatarSize_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "bad_avatar_size.yaml", `
postgres: { url: "postgres://x" }
s3: { endpoint: "http://minio:9000", root_user: "root", root_password: "rootpass", bucket: "avatars" }
avatar: { max_size_bytes: -666 }
`)
	_, err := Load(cfgPath)
	require.Error(t, err)
}

func TestMustLoad_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ok.yaml", minimalYAML)

	cfg := MustLoad(cfgPath)
	require.NotNil(t, cfg)
	require.Equal(t, "postgres://localhost/user-min", cfg.Postgres.URL)
	require.Equal(t, "avatars", cfg.S3.Bucket)
}

func TestMustLoad_PanicsOnError(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = MustLoad(filepath.Join(t.TempDir(), "nope.yaml"))
	})
}

func TestLoad_ZeroPresignTTL_UsesDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "ttl_zero.yaml", `
postgres: { url: "postgres://x" }
s3: {
  endpoint: "http://minio:9000",
  root_user: "root",
  root_password: "rootpass",
  bucket: "avatars",
  presign_ttl: "0s"
}
avatar: { max_size_bytes: 1, allowed_content_types: ["image/png"] }
`)
	cfg := MustLoad(cfgPath)
	require.Equal(t, 10*time.Minute, cfg.S3.PresignTTL)
}

func TestLoad_ZeroAvatarSize_UsesDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := writeFile(t, dir, "avatar_zero.yaml", `
postgres: { url: "postgres://x" }
s3: {
  endpoint: "http://minio:9000",
  root_user: "root",
  root_password: "rootpass",
  bucket: "avatars"
}
avatar: { max_size_bytes: 0, allowed_content_types: ["image/jpeg"] }
`)
	cfg := MustLoad(cfgPath)
	require.Equal(t, int64(5242880), cfg.Avatar.MaxSizeBytes)
}
