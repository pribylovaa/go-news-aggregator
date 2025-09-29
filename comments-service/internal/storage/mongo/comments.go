package mongo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// encodeCursor кодирует пару (created_at, _id) в непрозрачный токен для клиента.
func encodeCursor(time time.Time, id primitive.ObjectID) string {
	raw := fmt.Sprintf("%d|%s", time.UTC().UnixNano(), id.Hex())

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor декодирует токен обратно в пару ключей.
func decodeCursor(token string) (time.Time, primitive.ObjectID, error) {
	res, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return time.Time{}, primitive.NilObjectID, err
	}

	parts := strings.SplitN(string(res), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, primitive.NilObjectID, fmt.Errorf("bad parts")
	}

	nanos, err := parseInt64(parts[0])
	if err != nil {
		return time.Time{}, primitive.NilObjectID, err
	}

	oid, err := primitive.ObjectIDFromHex(parts[1])
	if err != nil {
		return time.Time{}, primitive.NilObjectID, err
	}

	return time.Unix(0, nanos).UTC(), oid, nil
}

// parseInt64 — локальная маленькая обёртка без импорта strconv везде.
func parseInt64(s string) (int64, error) {
	var x int64
	_, err := fmt.Sscan(s, &x)

	return x, err
}

// limitOrDefault приводит запрошенный размер страницы к [Default, Max].
func limitOrDefault(cfg *config.Config, pageSize int32) int64 {
	lim := pageSize
	if lim <= 0 {
		lim = cfg.Limits.Default
	}

	if lim > cfg.Limits.Max {
		lim = cfg.Limits.Max
	}

	return int64(lim)
}

// CreateComment создаёт комментарий (корневой или ответ).
//   - Для корня выставляет Level=0, ExpiresAt = now + cfg.TTL.Thread.
//   - Для ответа подтягивает NewsID/ExpiresAt из родителя, Level = parent.Level + 1.
//   - На родителе инкрементирует replies_count.
func (m *Mongo) CreateComment(ctx context.Context, comm models.Comment) (*models.Comment, error) {
	const op = "storage/mongo/CreateComment"

	// MongoDB DateTime хранит миллисекунды.
	toMS := func(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
	now := toMS(time.Now())

	// Базовая нормализация временных полей перед записью.
	comm.CreatedAt = now
	comm.UpdatedAt = now

	// Обработка корня/ответа.
	if strings.TrimSpace(comm.ParentID) == "" {
		// Корневой комментарий.
		comm.Level = 0
		comm.ExpiresAt = toMS(now.Add(m.cfg.TTL.Thread))
	} else {
		// Ответ: необходимо найти родителя и перенять часть полей/ограничений.
		parentOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(comm.ParentID))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrParentNotFound)
		}

		var parent models.Comment
		if err := m.comments.FindOne(ctx, bson.D{{Key: "_id", Value: parentOID}}).Decode(&parent); err != nil {
			if errors.Is(err, mongodriver.ErrNoDocuments) {
				return nil, fmt.Errorf("%s: %w", op, storage.ErrParentNotFound)
			}

			return nil, fmt.Errorf("%s: find parent: %w", op, err)
		}

		// Проверка глубины дерева.
		if parent.Level+1 > m.cfg.Limits.MaxDepth {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrMaxDepthExceeded)
		}

		// Если news_id не совпадает — принудительно выставим как у родителя (защита от рассинхрона).
		comm.NewsID = parent.NewsID

		// У ответов единый срок жизни ветки как у корня.
		comm.ExpiresAt = toMS(parent.ExpiresAt)
		comm.Level = parent.Level + 1

		// Инкремент счётчика у родителя по факту успешной вставки.
		defer func() {
			_, _ = m.comments.UpdateByID(ctx, parentOID, bson.D{
				{Key: "$inc", Value: bson.D{{Key: "replies_count", Value: 1}}},
				{Key: "$set", Value: bson.D{{Key: "updated_at", Value: toMS(time.Now())}}},
			})
		}()
	}

	// Вставляем документ. Если ID пустой — драйвер сгенерирует новый ObjectID.
	res, err := m.comments.InsertOne(ctx, comm)
	if err != nil {
		// Единственный реальный конфликт здесь — по _id, но мы доверяем драйверу.
		return nil, fmt.Errorf("%s: insert: %w", op, err)
	}

	oid, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		// Mongo всегда возвращает ObjectID.
		return nil, fmt.Errorf("%s: inserted id type", op)
	}

	comm.ID = oid.Hex()
	return &comm, nil
}

// DeleteComment помечает комментарий как удалённый (мягкое удаление).
// При отсутствии записи — storage.ErrNotFound.
func (m *Mongo) DeleteComment(ctx context.Context, id string) error {
	const op = "storage/mongo/DeleteComment"

	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	res, err := m.comments.UpdateByID(ctx, oid, bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "is_deleted", Value: true},
			{Key: "content", Value: ""},
			{Key: "updated_at", Value: time.Now().UTC()},
		}},
	})

	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if res.MatchedCount == 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	return nil
}

// CommentByID возвращает комментарий по идентификатору.
// Если запись не найдена — storage.ErrNotFound.
// Некорректный формат id трактуется как «нет такой записи».
func (m *Mongo) CommentByID(ctx context.Context, id string) (*models.Comment, error) {
	const op = "storage/mongo/CommentByID"

	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	var out models.Comment
	if err := m.comments.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&out); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Нормализация временных полей.
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()

	return &out, nil
}

// ListByNews возвращает страницу корневых комментариев новости (parent_id == "").
// Сортировка: created_at DESC, _id DESC.
// При некорректном page_token — storage.ErrInvalidCursor.
func (m *Mongo) ListByNews(ctx context.Context, newsID string, param models.ListParams) (*models.Page, error) {
	const op = "storage/mongo/ListByNews"

	newsUUID, err := uuid.Parse(strings.TrimSpace(newsID))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	limit := limitOrDefault(m.cfg, param.PageSize)

	filter := bson.D{
		{Key: "news_id", Value: newsUUID},
		{Key: "parent_id", Value: ""},
	}

	findOpts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(limit)

	// Курсор "меньше" для DESC сортировки.
	if strings.TrimSpace(param.PageToken) != "" {
		t, oid, decErr := decodeCursor(param.PageToken)
		if decErr != nil {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrInvalidCursor)
		}

		filter = append(filter, bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "created_at", Value: bson.D{{Key: "$lt", Value: t}}}},
			bson.D{
				{Key: "created_at", Value: t},
				{Key: "_id", Value: bson.D{{Key: "$lt", Value: oid}}},
			},
		}})
	}

	cur, err := m.comments.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("%s: find: %w", op, err)
	}
	defer cur.Close(ctx)

	var items []models.Comment
	for cur.Next(ctx) {
		var comm models.Comment
		if err := cur.Decode(&comm); err != nil {
			return nil, fmt.Errorf("%s: decode: %w", op, err)
		}

		// Нормализация времён.
		comm.CreatedAt = comm.CreatedAt.UTC()
		comm.UpdatedAt = comm.UpdatedAt.UTC()
		comm.ExpiresAt = comm.ExpiresAt.UTC()
		items = append(items, comm)
	}

	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("%s: cursor: %w", op, err)
	}

	var next string
	if n := len(items); n > 0 {
		last := items[n-1]
		// created_at и id всегда проставлены — соберём курсор.
		oid, _ := primitive.ObjectIDFromHex(last.ID)
		next = encodeCursor(last.CreatedAt, oid)
	}

	return &models.Page{
		Items:         items,
		NextPageToken: next,
	}, nil
}

// ListReplies возвращает страницу ответов для одной ветки (дети одного parent_id).
// Сортировка: created_at ASC, _id ASC — удобно для постепенной подзагрузки снизу вверх.
// При некорректном page_token — storage.ErrInvalidCursor.
func (m *Mongo) ListReplies(ctx context.Context, parentID string, param models.ListParams) (*models.Page, error) {
	const op = "storage/mongo/ListReplies"

	parentOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(parentID))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	limit := limitOrDefault(m.cfg, param.PageSize)

	filter := bson.D{
		{Key: "parent_id", Value: parentOID.Hex()},
	}

	findOpts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(limit)

	// Курсор "больше" для ASC сортировки.
	if strings.TrimSpace(param.PageToken) != "" {
		t, oid, decErr := decodeCursor(param.PageToken)
		if decErr != nil {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrInvalidCursor)
		}

		filter = append(filter, bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "created_at", Value: bson.D{{Key: "$gt", Value: t}}}},
			bson.D{
				{Key: "created_at", Value: t},
				{Key: "_id", Value: bson.D{{Key: "$gt", Value: oid}}},
			},
		}})
	}

	cur, err := m.comments.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("%s: find: %w", op, err)
	}
	defer cur.Close(ctx)

	var items []models.Comment
	for cur.Next(ctx) {
		var comm models.Comment
		if err := cur.Decode(&comm); err != nil {
			return nil, fmt.Errorf("%s: decode: %w", op, err)
		}

		comm.CreatedAt = comm.CreatedAt.UTC()
		comm.UpdatedAt = comm.UpdatedAt.UTC()
		comm.ExpiresAt = comm.ExpiresAt.UTC()
		items = append(items, comm)
	}

	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("%s: cursor: %w", op, err)
	}

	var next string
	if n := len(items); n > 0 {
		last := items[n-1]
		oid, _ := primitive.ObjectIDFromHex(last.ID)
		next = encodeCursor(last.CreatedAt, oid)
	}

	return &models.Page{
		Items:         items,
		NextPageToken: next,
	}, nil
}
