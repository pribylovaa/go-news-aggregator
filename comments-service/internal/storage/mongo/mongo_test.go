package mongo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// testTimeout — общий дедлайн на операции с БД в тестах.
const testTimeout = 10 * time.Second

// TestMain запускает MongoDB в контейнере один раз на весь пакет тестов.
// Адрес контейнера прокидывается в ENV DATABASE_URL, а каждая спецификация
// создаёт свою БД с уникальным именем (см. newTestConfig).
func TestMain(m *testing.M) {
	if os.Getenv("GO_TEST_INTEGRATION") == "" {
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := testcontainers.ContainerRequest{
		Image:        "mongo:7.0",
		ExposedPorts: []string{"27017/tcp"},
		WaitingFor:   wait.ForLog("Waiting for connections").WithStartupTimeout(90 * time.Second),
	}

	mongoC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start mongo testcontainer: %v\n", err)
		os.Exit(1)
	}

	// Получаем host:port и формируем URI без имени БД.
	host, err := mongoC.Host(ctx)
	if err != nil {
		_ = mongoC.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "failed to get container host: %v\n", err)
		os.Exit(1)
	}

	port, err := mongoC.MappedPort(ctx, "27017/tcp")
	if err != nil {
		_ = mongoC.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "failed to get mapped port: %v\n", err)
		os.Exit(1)
	}

	uri := fmt.Sprintf("mongodb://%s:%s", host, port.Port())
	_ = os.Setenv("DATABASE_URL", uri)

	// Запускаем тесты пакета.
	code := m.Run()

	// Гасим контейнер *после* выполнения пакета тестов.
	_ = mongoC.Terminate(context.Background())
	os.Exit(code)
}

// newTestConfig создаёт конфиг с отдельной тестовой БД.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()

	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		baseURL = "mongodb://localhost:27017"
	}

	dbName := "comments_test_" + uuid.New().String()
	if baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL + dbName
	} else {
		baseURL = baseURL + "/" + dbName
	}

	return &config.Config{
		DB: config.DBConfig{
			URL: baseURL,
		},
		TTL: config.TTLConfig{
			Thread: 24 * time.Hour,
		},
		Limits: config.LimitsConfig{
			Default:  2,
			Max:      100,
			MaxDepth: 3,
		},
	}
}

// mustNewMongo создаёт подключение к созданной Test DB и регистрирует очистку по завершении теста.
func mustNewMongo(t *testing.T, cfg *config.Config) *Mongo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	m, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("cannot connect to MongoDB in container: %v (DATABASE_URL=%s)", err, cfg.DB.URL)
	}

	// При завершении теста — подчистить БД и соединение.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		_ = m.db.Drop(ctx)
		_ = m.Close(ctx)
	})

	return m
}

// TestEncodeDecodeCursor — encode/decode должны быть взаимно обратимыми.
func TestEncodeDecodeCursor(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	oid := primitiveObjectIDForTest(t)

	token := encodeCursor(now, oid)
	gotT, gotID, err := decodeCursor(token)
	if err != nil {
		t.Fatalf("decodeCursor error: %v", err)
	}
	if !gotT.Equal(now) {
		t.Fatalf("time mismatch: want %v, got %v", now, gotT)
	}
	if gotID != oid {
		t.Fatalf("oid mismatch: want %v, got %v", oid, gotID)
	}
}

// TestLimitOrDefault — граничные случаи и дефолт для размера страницы.
func TestLimitOrDefault(t *testing.T) {
	cfg := &config.Config{
		Limits: config.LimitsConfig{Default: 10, Max: 50},
	}
	tests := []struct {
		name string
		in   int32
		want int64
	}{
		{"zero->default", 0, 10},
		{"negative->default", -5, 10},
		{"less-than-max", 25, 25},
		{"more-than-max->cap", 200, 50},
	}
	for _, tt := range tests {
		if got := limitOrDefault(cfg, tt.in); got != tt.want {
			t.Errorf("%s: want %d, got %d", tt.name, tt.want, got)
		}
	}
}

// TestCreateRootComment_SetsDefaults — проверяем выставление Level=0, корректный ExpiresAt и базовую инициализацию.
func TestCreateRootComment_SetsDefaults(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	comm := models.Comment{
		NewsID:   uuid.New(),
		UserID:   uuid.New(),
		Username: "alice",
		Content:  "hello world",
	}
	before := time.Now().UTC()

	out, err := m.CreateComment(ctx, comm)
	if err != nil {
		t.Fatalf("CreateComment(root) error: %v", err)
	}

	if out.ID == "" {
		t.Fatalf("expected generated ID")
	}

	if out.Level != 0 {
		t.Fatalf("root Level = %d, want 0", out.Level)
	}

	if out.ParentID != "" {
		t.Fatalf("root ParentID must be empty, got %q", out.ParentID)
	}

	// ExpiresAt должен быть в диапазоне (с учётом небольшого люфта).
	if !(out.ExpiresAt.After(before) && out.ExpiresAt.Before(before.Add(cfg.TTL.Thread+time.Minute))) {
		t.Fatalf("ExpiresAt not in [now, now+TTL]: %v", out.ExpiresAt)
	}

	if out.IsDeleted {
		t.Fatalf("IsDeleted unexpected true")
	}
}

// TestCreateReply_InheritsNewsAndTTL_IncrementsCounter — ответ наследует NewsID/ExpiresAt, поднимает Level и инкрементит replies_count у родителя.
func TestCreateReply_InheritsNewsAndTTL_IncrementsCounter(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	root, err := m.CreateComment(ctx, models.Comment{
		NewsID:   uuid.New(),
		UserID:   uuid.New(),
		Username: "bob",
		Content:  "root",
	})
	if err != nil {
		t.Fatalf("CreateComment(root) error: %v", err)
	}

	reply, err := m.CreateComment(ctx, models.Comment{
		// Даже если NewsID «левый» — в реплае он должен принудительно совпасть с родителем.
		NewsID:   uuid.New(),
		ParentID: root.ID,
		UserID:   uuid.New(),
		Username: "carol",
		Content:  "reply",
	})

	if err != nil {
		t.Fatalf("CreateComment(reply) error: %v", err)
	}

	if reply.ParentID != root.ID {
		t.Fatalf("reply.ParentID = %q, want %q", reply.ParentID, root.ID)
	}

	if reply.Level != root.Level+1 {
		t.Fatalf("reply.Level = %d, want %d", reply.Level, root.Level+1)
	}

	if reply.NewsID != root.NewsID {
		t.Fatalf("reply.NewsID = %s, want %s", reply.NewsID, root.NewsID)
	}

	if !reply.ExpiresAt.Equal(root.ExpiresAt) {
		t.Fatalf("reply.ExpiresAt = %v, want %v (inherited from root)", reply.ExpiresAt, root.ExpiresAt)
	}

	// Проверяем, что у родителя увеличился replies_count (инкремент после вставки).
	parent, err := m.CommentByID(ctx, root.ID)
	if err != nil {
		t.Fatalf("CommentByID(root) error: %v", err)
	}

	if parent.RepliesCount < 1 {
		t.Fatalf("parent.RepliesCount = %d, want >= 1", parent.RepliesCount)
	}
}

// TestCreateReply_ParentNotFound — при несуществующем parent ожидаем специализированную ошибку.
func TestCreateReply_ParentNotFound(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, err := m.CreateComment(ctx, models.Comment{
		NewsID:   uuid.New(),
		ParentID: "65e0a0c9fd2f000000000000", // валидный hex ObjectID, но документа нет.
		UserID:   uuid.New(),
		Username: "dave",
		Content:  "orphan",
	})

	if err == nil {
		t.Fatalf("want error when parent not found")
	}

	if !errors.Is(err, storage.ErrParentNotFound) && !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrParentNotFound, got %v", err)
	}
}

// TestCreateReply_MaxDepthExceeded — глубина ветки ограничена MaxDepth.
func TestCreateReply_MaxDepthExceeded(t *testing.T) {
	// Для быстроты выставим MaxDepth=1: root(0) -> first(1) -> second(2) => ошибка.
	cfg := newTestConfig(t)
	cfg.Limits.MaxDepth = 1
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	root, err := m.CreateComment(ctx, models.Comment{
		NewsID:   uuid.New(),
		UserID:   uuid.New(),
		Username: "root",
		Content:  "root",
	})

	if err != nil {
		t.Fatalf("CreateComment(root) error: %v", err)
	}

	first, err := m.CreateComment(ctx, models.Comment{
		ParentID: root.ID,
		UserID:   uuid.New(),
		Username: "u1",
		Content:  "first",
	})

	if err != nil {
		t.Fatalf("CreateComment(first) error: %v", err)
	}

	_, err = m.CreateComment(ctx, models.Comment{
		ParentID: first.ID,
		UserID:   uuid.New(),
		Username: "u2",
		Content:  "second",
	})

	if err == nil {
		t.Fatalf("want error on depth exceeded")
	}

	if !errors.Is(err, storage.ErrMaxDepthExceeded) {
		t.Logf("note: expected storage.ErrMaxDepthExceeded, got %v", err)
	}
}

// TestDeleteComment_SoftDelete — мягкое удаление: is_deleted=true и очищенный content.
func TestDeleteComment_SoftDelete(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	c, err := m.CreateComment(ctx, models.Comment{
		NewsID:   uuid.New(),
		UserID:   uuid.New(),
		Username: "z",
		Content:  "to be deleted",
	})

	if err != nil {
		t.Fatalf("CreateComment(root) error: %v", err)
	}

	if err := m.DeleteComment(ctx, c.ID); err != nil {
		t.Fatalf("DeleteComment error: %v", err)
	}

	got, err := m.CommentByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("CommentByID after delete error: %v", err)
	}

	if !got.IsDeleted || got.Content != "" {
		t.Fatalf("soft delete failed: is_deleted=%v, content=%q", got.IsDeleted, got.Content)
	}
}

// TestCommentByID_NotFoundOnBadID — невалидный формат id трактуем как отсутствие записи.
func TestCommentByID_NotFoundOnBadID(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, err := m.CommentByID(ctx, "deadbeef")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want ErrNotFound for bad id format, got %v", err)
	}
}

// TestListByNews_PaginationAndOrder — проверяем порядок (DESC) и курсорную пагинацию корневых комментариев.
func TestListByNews_PaginationAndOrder(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	newsID := uuid.New()
	// Создаём 3 корневых комментария с паузами -> однозначный порядок по created_at.
	for i := 0; i < 3; i++ {
		_, err := m.CreateComment(ctx, models.Comment{
			NewsID:   newsID,
			UserID:   uuid.New(),
			Username: "u",
			Content:  "root " + uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("CreateComment(root %d) error: %v", i, err)
		}

		time.Sleep(10 * time.Millisecond)
	}

	// Страница 1: size=2, порядок DESC (сначала новые).
	p1, err := m.ListByNews(ctx, newsID.String(), models.ListParams{PageSize: 2})
	if err != nil {
		t.Fatalf("ListByNews page1 error: %v", err)
	}

	if len(p1.Items) != 2 {
		t.Fatalf("page1 len=%d, want 2", len(p1.Items))
	}

	if p1.NextPageToken == "" {
		t.Fatalf("page1 must have next token")
	}

	// created_at должен быть убывающим (не позже следующего).
	if !p1.Items[0].CreatedAt.After(p1.Items[1].CreatedAt) && !p1.Items[0].CreatedAt.Equal(p1.Items[1].CreatedAt) {
		t.Fatalf("order DESC violated: %v THEN %v", p1.Items[0].CreatedAt, p1.Items[1].CreatedAt)
	}

	// Страница 2: добираем остаток (1 шт).
	p2, err := m.ListByNews(ctx, newsID.String(), models.ListParams{PageToken: p1.NextPageToken, PageSize: 2})
	if err != nil {
		t.Fatalf("ListByNews page2 error: %v", err)
	}

	if len(p2.Items) != 1 {
		t.Fatalf("page2 len=%d, want 1", len(p2.Items))
	}

	// Битый токен -> ErrInvalidCursor.
	if _, err := m.ListByNews(ctx, newsID.String(), models.ListParams{PageToken: "!!!"}); !errors.Is(err, storage.ErrInvalidCursor) {
		t.Fatalf("want ErrInvalidCursor on bad token, got %v", err)
	}
}

// TestListReplies_PaginationAndOrder — проверяем порядок (ASC) и курсорную пагинацию ответов.
func TestListReplies_PaginationAndOrder(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	root, err := m.CreateComment(ctx, models.Comment{
		NewsID:   uuid.New(),
		UserID:   uuid.New(),
		Username: "u",
		Content:  "root",
	})

	if err != nil {
		t.Fatalf("CreateComment(root) error: %v", err)
	}

	// 3 ответа с паузой -> однозначный порядок по created_at (старые первыми).
	for i := 0; i < 3; i++ {
		_, err := m.CreateComment(ctx, models.Comment{
			ParentID: root.ID,
			UserID:   uuid.New(),
			Username: "u",
			Content:  "reply " + uuid.NewString(),
		})

		if err != nil {
			t.Fatalf("CreateComment(reply %d) error: %v", i, err)
		}

		time.Sleep(10 * time.Millisecond)
	}

	p1, err := m.ListReplies(ctx, root.ID, models.ListParams{PageSize: 2})
	if err != nil {
		t.Fatalf("ListReplies page1 error: %v", err)
	}

	if len(p1.Items) != 2 {
		t.Fatalf("page1 len=%d, want 2", len(p1.Items))
	}

	if p1.NextPageToken == "" {
		t.Fatalf("page1 must have next token")
	}

	// Порядок ASC: первый элемент не позже второго.
	if p1.Items[0].CreatedAt.After(p1.Items[1].CreatedAt) {
		t.Fatalf("order ASC violated: %v THEN %v", p1.Items[0].CreatedAt, p1.Items[1].CreatedAt)
	}

	p2, err := m.ListReplies(ctx, root.ID, models.ListParams{PageToken: p1.NextPageToken, PageSize: 2})
	if err != nil {
		t.Fatalf("ListReplies page2 error: %v", err)
	}

	if len(p2.Items) != 1 {
		t.Fatalf("page2 len=%d, want 1", len(p2.Items))
	}

	// Битый токен -> ErrInvalidCursor.
	if _, err := m.ListReplies(ctx, root.ID, models.ListParams{PageToken: "!!!"}); !errors.Is(err, storage.ErrInvalidCursor) {
		t.Fatalf("want ErrInvalidCursor on bad token, got %v", err)
	}
}

// TestEnsureIndexes_Created — индексы, создаваемые ensureIndexes, существуют.
// Проверяем как по имени (если задано), так и по составу ключей — чтобы быть устойчивыми
// к различиям в авто-именовании.
func TestEnsureIndexes_Created(t *testing.T) {
	cfg := newTestConfig(t)
	m := mustNewMongo(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cur, err := m.comments.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("Indexes().List error: %v", err)
	}
	defer cur.Close(ctx)

	type keyDoc = map[string]any
	haveNames := map[string]bool{}
	var haveTTL, haveRootList, haveRepliesList bool

	for cur.Next(ctx) {
		var spec map[string]any
		if err := cur.Decode(&spec); err != nil {
			t.Fatalf("decode index spec: %v", err)
		}

		if name, _ := spec["name"].(string); name != "" {
			haveNames[name] = true
		}

		// Проверяем состав ключей.
		if k, ok := spec["key"].(map[string]any); ok {
			// TTL: expires_at: 1
			if len(k) == 1 && numEq(k["expires_at"], 1) {
				haveTTL = true
			}

			// Корневые: news_id:1, parent_id:1, created_at:-1
			if numEq(k["news_id"], 1) && numEq(k["parent_id"], 1) && numEq(k["created_at"], -1) {
				haveRootList = true
			}

			// Ответы: parent_id:1, created_at:1
			if numEq(k["parent_id"], 1) && numEq(k["created_at"], 1) && k["news_id"] == nil {
				haveRepliesList = true
			}
		}
	}

	if err := cur.Err(); err != nil {
		t.Fatalf("cursor err: %v", err)
	}

	// Разрешаем как проверку по имени (если явно задано в ensureIndexes), так и по составу ключей.
	byNameOK := haveNames["ttl_expires_at"] && haveNames["news_parent_created_desc"] && haveNames["parent_created_asc"]
	byKeysOK := haveTTL && haveRootList && haveRepliesList

	if !(byNameOK || byKeysOK) {
		t.Fatalf("required indexes not found; names=%v, ttl=%v, root=%v, replies=%v", haveNames, haveTTL, haveRootList, haveRepliesList)
	}
}

// primitiveObjectIDForTest возвращает новый ObjectID (используем для проверки курсора).
func primitiveObjectIDForTest(t *testing.T) primitive.ObjectID {
	t.Helper()
	return primitive.NewObjectID()
}

// numEq — безопасное сравнение числовых значений из BSON спецификаций индексов.
func numEq(v any, want int) bool {
	switch n := v.(type) {
	case int:
		return n == want
	case int32:
		return int(n) == want
	case int64:
		return int(n) == want
	case float64:
		return int(n) == want
	default:
		return false
	}
}
