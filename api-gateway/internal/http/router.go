package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/http/handlers"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/http/middleware"
)

// Options — параметры сборки HTTP-роутера.
type Options struct {
	Logger   *slog.Logger
	Timeout  time.Duration
	BasePath string // например, "/api"; если пустой — роуты регистрируются на корне.
}

// NewRouter собирает http.Handler с chi и подключёнными middleware/роутами.
func NewRouter(cl *clients.Clients, opts Options) http.Handler {
	root := chi.NewRouter()

	// Middleware (внешний -> внутренний).
	root.Use(
		middleware.Recover(),            // безопасно ловим паники
		middleware.RequestID(),          // формируем/прокидываем X-Request-Id (до логирования!)
		middleware.Logging(opts.Logger), // кладём request-scoped логгер в контекст и логируем
		middleware.AuthBearer(),         // вынимаем Bearer токен в контекст для gRPC-клиентов
	)
	if opts.Timeout > 0 {
		root.Use(middleware.Timeout(opts.Timeout)) // общий дедлайн запроса
	}

	// Зависимости хендлеров.
	h := handlers.New(cl)

	// Регистрация маршрутов.
	if opts.BasePath != "" {
		sub := chi.NewRouter()
		registerRoutes(sub, h)
		root.Mount(opts.BasePath, sub)
		return root
	}

	registerRoutes(root, h)
	return root
}

// registerRoutes — единая точка регистрации всех REST-эндпойнтов.
func registerRoutes(r chi.Router, h *handlers.Handlers) {
	// auth
	r.Post("/auth/register", h.RegisterUser)
	r.Post("/auth/login", h.LoginUser)
	r.Post("/auth/refresh", h.RefreshToken)
	r.Post("/auth/revoke", h.RevokeToken)
	r.Post("/auth/validate", h.ValidateToken)

	// news
	r.Get("/news", h.ListNews)
	r.Get("/news/{id}", h.GetNewsByID)

	// comments
	r.Post("/comments", h.CreateComment)
	r.Get("/comments/{id}", h.GetCommentByID)
	r.Get("/news/{news_id}/comments", h.ListRootComments)
	r.Get("/comments/{id}/replies", h.ListReplies)

	// users
	r.Get("/users/{id}", h.GetProfile)
	r.Patch("/users/{id}", h.UpdateProfile)
	r.Post("/users/{id}/avatar/presign", h.AvatarPresign)
	r.Post("/users/{id}/avatar/confirm", h.AvatarConfirm)
}
