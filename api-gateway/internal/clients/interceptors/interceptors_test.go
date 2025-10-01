package interceptors

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"
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

func TestClientMetadata_AppendsHeaders(t *testing.T) {
	t.Parallel()

	const rid = "rid-123"
	const tok = "token-xyz"
	const ua = "api-gateway"

	ctx := context.WithValue(context.Background(), CtxRequestID, rid)
	ctx = context.WithValue(ctx, CtxAuthToken, tok)

	mdOut := metadata.MD{}
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		mdOut = md
		return nil
	}

	inter := ClientWithMetadata(ua)
	err := inter(ctx, "/auth.AuthService/Register", "req", nil, nil, invoker)
	require.NoError(t, err)

	require.Equal(t, []string{rid}, mdOut.Get("x-request-id"))
	require.Equal(t, []string{"Bearer " + tok}, mdOut.Get("authorization"))
	require.Equal(t, []string{ua}, mdOut.Get("user-agent"))
}

func TestClientMetadata_SkipEmptyValues(t *testing.T) {
	t.Parallel()

	inter := ClientWithMetadata("") // пустой UA

	var mdOut metadata.MD
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		mdOut = md
		return nil
	}

	err := inter(context.Background(), "/news.NewsService/List", nil, nil, nil, invoker)
	require.NoError(t, err)
	require.Empty(t, mdOut.Get("x-request-id"))
	require.Empty(t, mdOut.Get("authorization"))
	require.Empty(t, mdOut.Get("user-agent"))
}

func TestClientWithTimeout_SetsDeadline_AndInvokerSeesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	const d = 40 * time.Millisecond
	inter := ClientWithTimeout(d)

	start := time.Now()
	err := inter(
		context.Background(),
		"/auth.AuthService/Sleep",
		nil, nil, nil,
		func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			<-ctx.Done()
			return ctx.Err()
		},
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, time.Since(start), d)
}

func TestClientWithTimeout_DoesNotOverrideExistingDeadline(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	parentDL, ok := parent.Deadline()
	require.True(t, ok)

	inter := ClientWithTimeout(1 * time.Second)

	var childDL time.Time
	err := inter(
		parent,
		"/users.UsersService/Call",
		nil, nil, nil,
		func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			var ok bool
			childDL, ok = ctx.Deadline()
			require.True(t, ok)
			return nil
		},
	)
	require.NoError(t, err)
	require.WithinDuration(t, parentDL, childDL, time.Millisecond)
}

func TestClientWithTimeout_ZeroDuration_PassThrough(t *testing.T) {
	t.Parallel()

	inter := ClientWithTimeout(0)
	var hasDL bool
	err := inter(
		context.Background(),
		"/comments.CommentsService/Ping",
		nil, nil, nil,
		func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			_, hasDL = ctx.Deadline()
			return nil
		},
	)
	require.NoError(t, err)
	require.False(t, hasDL, "no deadline expected when d <= 0")
}

func TestClientUnaryLoggingInterceptor_LogsAndPutsLoggerIntoContext(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	base := slog.New(h)

	// Для реалистичности добавим peer в контекст (хотя клиентский интерсептор может не использовать его).
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50051},
	})

	inter := ClientUnaryLoggingInterceptor(base)

	err := inter(ctx, "/auth.AuthService/Login", "req", nil, nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		// Логгер внедрён в контекст.
		l := log.From(ctx)
		l.Info("probe", slog.String("ok", "1"))
		return nil
	})
	require.NoError(t, err)

	// Была запись "probe" от хендлера.
	require.Equal(t, 1, h.count["probe"])

	// Итоговая запись интерсептора.
	require.Equal(t, "grpc", h.lastMsg)
	require.Equal(t, slog.LevelInfo, h.lastLvl)

	// Ожидаем как минимум наличие кода и длительности.
	code, _ := h.attrs["code"].(string)
	require.NotEmpty(t, code)
	require.Contains(t, []string{"OK"}, code)

	if d, ok := h.attrs["dur"].(time.Duration); ok {
		require.Greater(t, d, time.Duration(0))
	} else {
		t.Fatalf("dur attr not found or wrong type: %#v", h.attrs["dur"])
	}
}

func TestClientUnaryLoggingInterceptor_PropagatesErrorCode(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	inter := ClientUnaryLoggingInterceptor(logger)

	err := inter(context.Background(), "/auth.AuthService/Foo", nil, nil, nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return status.Error(codes.InvalidArgument, "bad input")
	})
	require.Error(t, err)

	require.Equal(t, "grpc", h.lastMsg)
	require.Equal(t, "InvalidArgument", h.attrs["code"])
}

// Если ClientUnaryLoggingInterceptor добавляет request_id — проверим UUID.
func TestClientUnaryLoggingInterceptor_RequestIDIfPresent_IsUUID(t *testing.T) {
	t.Parallel()

	h := &capHandler{}
	logger := slog.New(h)

	// Если в лог добавляется request_id — пусть будет валидным UUID.
	inter := ClientUnaryLoggingInterceptor(logger)
	_ = inter(context.Background(), "/news.NewsService/List", nil, nil, nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	})

	if rid, ok := h.attrs["request_id"].(string); ok && rid != "" {
		_, err := uuid.Parse(rid)
		require.NoError(t, err)
	}
}
