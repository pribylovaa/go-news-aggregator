package mongo

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const (
	commentsCollection = "comments"
	defaultDBName      = "comments"
)

// Mongo - тонкий адаптер для подключения и коллекций MongoDB.
type Mongo struct {
	cfg      *config.Config
	client   *mongodriver.Client
	db       *mongodriver.Database
	comments *mongodriver.Collection
}

// New подключается к MongoDB, проверяет его, подготавливает коллекции и обеспечивает индексацию.
func New(ctx context.Context, cfg *config.Config) (*Mongo, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mongo: nil config")
	}

	if cfg.DB.URL == "" {
		return nil, fmt.Errorf("mongo: empty cfg.DB.URL")
	}

	cli, err := mongodriver.Connect(ctx, options.Client().ApplyURI(cfg.DB.URL))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := cli.Ping(ctx, readpref.Primary()); err != nil {
		_ = cli.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	dbName := databaseFromURI(cfg.DB.URL)
	db := cli.Database(dbName)

	m := &Mongo{
		cfg:      cfg,
		client:   cli,
		db:       db,
		comments: db.Collection(commentsCollection),
	}

	if err := m.ensureIndexes(ctx); err != nil {
		_ = m.Close(ctx)
		return nil, err
	}

	return m, nil
}

func (m *Mongo) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

// ensureIndexes создает индексы, необходимые для службы комментариев.
// - TTL по expires_at (expireAfterSeconds=0 -> используется временная метка, сохраненненная в документе)
// - Список корневых комментариев: news_id + parent_id + created_at(desc)
// - Ответы в теме: parent_id + created_at(asc)
func (m *Mongo) ensureIndexes(ctx context.Context) error {

	models := []mongodriver.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0),
		},
		{
			Keys:    bson.D{{Key: "news_id", Value: 1}, {Key: "parent_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("news_parent_created_desc"),
		},
		{
			Keys:    bson.D{{Key: "parent_id", Value: 1}, {Key: "created_at", Value: 1}},
			Options: options.Index().SetName("parent_created_asc"),
		},
	}

	_, err := m.comments.Indexes().CreateMany(ctx, models)
	if err != nil {
		return fmt.Errorf("mongo ensure indexes: %w", err)
	}
	return nil
}

// databaseFromURI извлекает имя базы данных из URI-пути mongodb.
// Если оно отсутствует или не поддается расшифровке, возвращает разумное значение по умолчанию.
func databaseFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err == nil {
		if name := strings.Trim(u.Path, "/"); name != "" {
			return name
		}
	}
	return defaultDBName
}
