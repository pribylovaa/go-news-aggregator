package interceptors

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type capHandler struct {
	base    []slog.Attr
	lastMsg string
	lastLvl slog.Level
	attrs   map[string]any
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

func TestUnaryLoggingInterceptor_Success_WithRequestID(t *testing.T) {
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

func TestUnaryLoggingInterceptor_GeneratesUUID_And_LogsErrorCode(t *testing.T) {
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

func TestRecover_PanicToInternal_AndLogsStack(t *testing.T) {
	h := &capHandler{}
	logger := slog.New(h)

	info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/WillPanic"}
	inter := Recover(logger)

	resp, err := inter(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		panic("boom")
	})

	// RPC-ответ.
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))

	// Логи от Recover: уровень error, сообщение и атрибуты.
	require.Equal(t, slog.LevelError, h.lastLvl)
	require.Equal(t, "panic recovered", h.lastMsg)

	method, ok := h.attrs["method"].(string)
	require.True(t, ok)
	require.Equal(t, info.FullMethod, method)

	// panic: хранится как Any — это будет "boom".
	require.NotEmpty(t, h.attrs["panic"])

	// stack: непустая строка с трассировкой.
	stack, ok := h.attrs["stack"].(string)
	require.True(t, ok)
	require.NotEmpty(t, stack)
}

func TestRecover_NoPanic_PassThrough_NoLogs(t *testing.T) {
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

func TestWithTimeout_SetsDeadline_AndHandlerSeesDeadlineExceeded(t *testing.T) {
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

func TestWithTimeout_DoesNotOverrideExistingDeadline(t *testing.T) {
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

func TestWithTimeout_ZeroDuration_PassThrough(t *testing.T) {
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
