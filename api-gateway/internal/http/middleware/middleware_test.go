package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients/interceptors"
	"github.com/stretchr/testify/require"
)

// capHandler — тестовый slog.Handler, который:
//   - аккумулирует базовые attrs, приходящие через Logger.With(...);
//   - собирает attrs из каждой записи в map[string]any;
//   - не создаёт реальных I/O, чтобы не паниковать в тестах.
type capHandler struct {
	base    []slog.Attr
	lastMsg string
	lastLvl slog.Level
	attrs   map[string]any
	count   int
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

	h.count++
	h.lastMsg = r.Message
	h.lastLvl = r.Level
	h.attrs = out

	return nil
}

func (h *capHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) > 0 {
		h.base = append(h.base, attrs...)
	}

	return h
}

func (h *capHandler) WithGroup(string) slog.Handler { return h }

func makeReq(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = (&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}).String()
	return req
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type errEnvelope struct {
	Error apiError `json:"error"`
}

func TestChain_Order(t *testing.T) {
	order := []string{}

	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m1-begin")
			next.ServeHTTP(w, r)
			order = append(order, "m1-end")
		})
	}

	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m2-begin")
			next.ServeHTTP(w, r)
			order = append(order, "m2-end")
		})
	}

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusTeapot)
	})

	chain := Chain(final, m1, m2)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, makeReq("/chain"))

	require.Equal(t, []string{"m1-begin", "m2-begin", "handler", "m2-end", "m1-end"}, order)
	require.Equal(t, http.StatusTeapot, rr.Code)
}

func TestRequestID_GenerateAndPropagate(t *testing.T) {
	var seenID string
	var seenCtxID string

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenID = r.Header.Get("X-Request-Id")
		if v := r.Context().Value(interceptors.CtxRequestID); v != nil {
			seenCtxID, _ = v.(string)
		}
		w.WriteHeader(http.StatusOK)
	})

	chain := Chain(h, RequestID())
	rr := httptest.NewRecorder()
	req := makeReq("/rid")
	chain.ServeHTTP(rr, req)

	respID := rr.Header().Get("X-Request-Id")
	require.NotEmpty(t, respID)
	require.Len(t, respID, 32) // 16 байт → 32 hex-символа

	require.Equal(t, respID, seenID)
	require.Equal(t, respID, seenCtxID)
}

func TestRequestID_UseExisting(t *testing.T) {
	const given = "abc123-existing-id"
	var seenCtxID string

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Context().Value(interceptors.CtxRequestID); v != nil {
			seenCtxID, _ = v.(string)
		}
		w.WriteHeader(http.StatusOK)
	})

	chain := Chain(h, RequestID())
	rr := httptest.NewRecorder()
	req := makeReq("/rid2")
	req.Header.Set("X-Request-Id", given)
	chain.ServeHTTP(rr, req)

	require.Equal(t, given, rr.Header().Get("X-Request-Id"))
	require.Equal(t, given, seenCtxID)
}

func TestAuthBearer_PopulatesContext_WhenBearerPresent(t *testing.T) {
	var token string

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Context().Value(interceptors.CtxAuthToken); v != nil {
			token, _ = v.(string)
		}
		w.WriteHeader(http.StatusOK)
	})

	chain := Chain(h, AuthBearer())
	rr := httptest.NewRecorder()
	req := makeReq("/auth")
	req.Header.Set("Authorization", "Bearer test-token-123")
	chain.ServeHTTP(rr, req)

	require.Equal(t, "test-token-123", token)
}

func TestAuthBearer_IgnoresInvalidHeader(t *testing.T) {
	var found bool

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, found = r.Context().Value(interceptors.CtxAuthToken).(string)
		w.WriteHeader(http.StatusOK)
	})
	chain := Chain(h, AuthBearer())

	// 1) Пусто.
	rr := httptest.NewRecorder()
	req := makeReq("/auth1")
	chain.ServeHTTP(rr, req)
	require.False(t, found)

	// 2) Без префикса Bearer.
	rr = httptest.NewRecorder()
	req = makeReq("/auth2")
	req.Header.Set("Authorization", "Basic aaa")
	chain.ServeHTTP(rr, req)
	require.False(t, found)
}

func TestTimeout_SetsDeadline_WhenAbsent(t *testing.T) {
	var hasDeadline bool
	var left time.Duration

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dl, ok := r.Context().Deadline()
		hasDeadline = ok
		if ok {
			left = time.Until(dl)
		}
		w.WriteHeader(http.StatusOK)
	})

	chain := Chain(h, Timeout(50*time.Millisecond))
	rr := httptest.NewRecorder()
	req := makeReq("/timeout")
	chain.ServeHTTP(rr, req)

	require.True(t, hasDeadline)
	require.Greater(t, left, time.Duration(0))
}

func TestTimeout_DoesNotOverrideExistingDeadline(t *testing.T) {
	var childDL time.Time

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dl, _ := r.Context().Deadline()
		childDL = dl
		w.WriteHeader(http.StatusOK)
	})

	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req := makeReq("/timeout2").WithContext(parent)

	chain := Chain(h, Timeout(1*time.Second)) // больше, чем у родителя
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)

	parentDL, _ := parent.Deadline()
	require.WithinDuration(t, parentDL, childDL, time.Millisecond)
}

func TestRecover_ConvertsPanicTo500(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	chain := Chain(panicHandler, Recover())
	rr := httptest.NewRecorder()
	req := makeReq("/panic")

	chain.ServeHTTP(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var env errEnvelope
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	require.Equal(t, "internal", env.Error.Code)
	require.NotEmpty(t, env.Error.Message)
}

func TestLogging_WritesRecord_WithStatusDurBytesAndRequestID(t *testing.T) {
	h := &capHandler{}
	logger := slog.New(h)

	// Ручной request id обеспечит присутствие request_id в логах.
	const rid = "rid-456"
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Не вызываем WriteHeader — статус должен стать 200 после Write.
		_, _ = w.Write([]byte("0123456789")) // 10 байт
	})

	// Порядок важен: RequestID до Logging, чтобы id попал в attrs лога.
	handler := Chain(final, RequestID(), Logging(logger))

	rr := httptest.NewRecorder()
	req := makeReq("/log")
	req.Header.Set("X-Request-Id", rid)

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, 1, h.count)
	require.Equal(t, "http", h.lastMsg)

	// Проверяем ключевые атрибуты.
	method, _ := h.attrs["method"].(string)
	path, _ := h.attrs["path"].(string)
	status, _ := h.attrs["status"].(int64) // slog хранит числа как int64
	bytes, _ := h.attrs["bytes"].(int64)
	ridAttr, _ := h.attrs["request_id"].(string)

	require.Equal(t, http.MethodGet, method)
	require.Equal(t, "/log", path)
	require.EqualValues(t, http.StatusOK, status)
	require.EqualValues(t, 10, bytes)
	require.Equal(t, rid, ridAttr)

	// Длительность > 0.
	_, hasDur := h.attrs["dur"]
	require.True(t, hasDur)
}

func TestStatusWriter_CountsBytes_AndDefaultStatus200(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := newStatusWriter(rr)

	_, _ = sw.Write([]byte("abcd")) // 4 байта

	require.Equal(t, http.StatusOK, sw.status) // статус умолчаний — 200
	require.Equal(t, 4, sw.count)
}
