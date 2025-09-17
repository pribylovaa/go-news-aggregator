package interceptors

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/pkg/log"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Пакет unit-тестов для internal/interceptors (timeout.go, recover.go, logging.go).

// capHandler — минимальный slog.Handler для захвата последней записи
// и всех атрибутов. Дополнительно ведёт счётчик сообщений по тексту.
type capHandler struct {
	base    []slog.Attr
	lastMsg string
	lastLvl slog.Level
	attrs   map[string]any
	count   map[string]int
}

func (h *capHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capHandler) Handle(_ context.Context, r slog.Record) error {
	out := make(map[string]any, len(h.base)+8)
	for _, a := range h.base {
		out[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Any()
		return true
	})
	if h.count == nil {
		h.count = make(map[string]int)
	}
	h.count[r.Message]++
	h.lastMsg = r.Message
	h.lastLvl = r.Level
	h.attrs = out
	return nil
}

func (h *capHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.base = append(h.base, attrs...)
	return h
}

func (h *capHandler) WithGroup(string) slog.Handler { return h }

// TestUnaryLoggingInterceptor_Success_WithRequestID —
// happy-path: берёт x-request-id из metadata, логирует метод/peer/код/длительность.
func TestUnaryLoggingInterceptor_Success_WithRequestID(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	md := metadata.New(map[string]string{"x-request-id": "rid-123"})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = peer.NewContext(ctx, &peer.Peer{
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50051},
	})

	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/Register"}

	inter := UnaryLoggingInterceptor(logger)
	resp, err := inter(ctx, "req", info, func(ctx context.Context, req any) (any, error) {
		time.Sleep(5 * time.Millisecond)
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	require.Equal(t, "grpc", h.lastMsg)
	require.Equal(t, slog.LevelInfo, h.lastLvl)
	require.Equal(t, "rid-123", h.attrs["request_id"])
	require.Equal(t, info.FullMethod, h.attrs["method"])
	require.Equal(t, "127.0.0.1:50051", h.attrs["peer"])
	require.Equal(t, "OK", h.attrs["code"])

	if d, ok := h.attrs["dur"].(time.Duration); ok {
		require.Greater(t, d, time.Duration(0))
	} else {
		t.Fatalf("dur attr not found or wrong type: %#v", h.attrs["dur"])
	}
}

// TestUnaryLoggingInterceptor_GeneratesUUID_And_LogsErrorCode —
// без x-request-id генерируется UUID; код ошибки берётся из status.
func TestUnaryLoggingInterceptor_GeneratesUUID_And_LogsErrorCode(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/Foo"}
	inter := UnaryLoggingInterceptor(logger)

	_, err := inter(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.InvalidArgument, "bad input")
	})
	require.Error(t, err)

	require.Equal(t, "grpc", h.lastMsg)
	require.Equal(t, "InvalidArgument", h.attrs["code"])

	rid, _ := h.attrs["request_id"].(string)
	require.NotEmpty(t, rid)
	_, parseErr := uuid.Parse(rid)
	require.NoError(t, parseErr)
}

// TestUnaryLoggingInterceptor_PutsLoggerIntoContext —
// интерсептор кладёт обогащённый *slog.Logger в context (pkg/log).
func TestUnaryLoggingInterceptor_PutsLoggerIntoContext(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	base := slog.New(h)

	md := metadata.New(map[string]string{"x-request-id": "abc"})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/UseLogger"}

	inter := UnaryLoggingInterceptor(base)

	_, err := inter(ctx, "req", info, func(ctx context.Context, req any) (any, error) {
		l := log.From(ctx)
		l.Info("handler", slog.String("probe", "1"))
		return "ok", nil
	})
	require.NoError(t, err)

	require.Equal(t, 1, h.count["handler"])
	require.Equal(t, "grpc", h.lastMsg)
	require.Equal(t, "abc", h.attrs["request_id"])
	require.Equal(t, "/auth.AuthService/UseLogger", h.attrs["method"])
}

// TestUnaryLoggingInterceptor_NoPeer_SetsDash —
// при отсутствии peer в контексте пишет "-" в лог.
func TestUnaryLoggingInterceptor_NoPeer_SetsDash(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	md := metadata.New(map[string]string{"x-request-id": "rid"})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/NoPeer"}

	inter := UnaryLoggingInterceptor(logger)
	_, err := inter(ctx, "req", info, func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "-", h.attrs["peer"])
}

// TestRecover_PanicToInternal_AndLogsStack —
// паника преобразуется в codes.Internal и логируется с методом и стеком.
func TestRecover_PanicToInternal_AndLogsStack(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/WillPanic"}
	inter := Recover(logger)

	resp, err := inter(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		panic("boom")
	})

	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))

	require.Equal(t, slog.LevelError, h.lastLvl)
	require.Equal(t, "panic_recovered", h.lastMsg)

	method, ok := h.attrs["method"].(string)
	require.True(t, ok)
	require.Equal(t, info.FullMethod, method)

	require.NotEmpty(t, h.attrs["panic"])

	stack, ok := h.attrs["stack"].(string)
	require.True(t, ok)
	require.NotEmpty(t, stack)
}

// TestRecover_NoPanic_PassThrough_NoLogs —
// если паники нет — ответ passthrough, логов от Recover нет.
func TestRecover_NoPanic_PassThrough_NoLogs(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/OK"}
	inter := Recover(logger)

	resp, err := inter(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	})

	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	require.Equal(t, "", h.lastMsg)
}

// TestWithTimeout_SetsDeadline_AndHandlerSeesDeadlineExceeded —
// навешивает дедлайн при его отсутствии, handler видит context.DeadlineExceeded.
func TestWithTimeout_SetsDeadline_AndHandlerSeesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	const d = 40 * time.Millisecond
	inter := WithTimeout(d)

	start := time.Now()
	_, err := inter(
		context.Background(),
		"req",
		&grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/Sleep"},
		func(ctx context.Context, req any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, time.Since(start), d)
}

// TestWithTimeout_DoesNotOverrideExistingDeadline —
// существующий дедлайн не переопределяется.
func TestWithTimeout_DoesNotOverrideExistingDeadline(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	pdl, ok := parent.Deadline()
	require.True(t, ok)

	inter := WithTimeout(1 * time.Second)

	var childDL time.Time
	resp, err := inter(
		parent,
		"req",
		&grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/HasDeadline"},
		func(ctx context.Context, req any) (any, error) {
			var ok bool
			childDL, ok = ctx.Deadline()
			require.True(t, ok)
			return "ok", nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.WithinDuration(t, pdl, childDL, time.Millisecond)
}

// TestWithTimeout_ZeroDuration_PassThrough —
// d<=0 -> не меняет контекст и не задаёт дедлайн.
func TestWithTimeout_ZeroDuration_PassThrough(t *testing.T) {
	t.Parallel()

	inter := WithTimeout(0)

	resp, err := inter(
		context.Background(),
		"req",
		&grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/NoTimeout"},
		func(ctx context.Context, req any) (any, error) {
			_, hasDL := ctx.Deadline()
			require.False(t, hasDL, "no deadline expected when d <= 0")
			return "ok", nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}
