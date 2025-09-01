package log

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func newSilent() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFrom_ReturnsDefault_WhenNoLoggerInContext(t *testing.T) {
	// Сохраняем старый default и восстанавливаем по окончании.
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	def := newSilent()
	slog.SetDefault(def)

	got := From(context.Background())
	require.Equal(t, def, got, "From должен вернуть slog.Default(), если в контексте ничего нет")
}

func TestIntoAndFrom_RoundTrip(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	def := newSilent()
	slog.SetDefault(def)

	l := newSilent()
	ctx := Into(context.Background(), l)

	require.Equal(t, l, From(ctx))

	require.Equal(t, def, From(context.Background()))
}

func TestFrom_ReturnsDefault_WhenStoredValueIsWrongTypeOrNil(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	def := newSilent()
	slog.SetDefault(def)

	// 1) В контексте лежит значение "не того типа" под тем же ключом.
	ctxWrong := context.WithValue(context.Background(), ctxKey{}, "not-a-logger")
	got := From(ctxWrong)
	require.Equal(t, def, got, "ожидаем slog.Default() при неверном типе значения")

	// 2) В контексте лежит *slog.Logger == nil.
	var nilLogger *slog.Logger
	ctxNil := context.WithValue(context.Background(), ctxKey{}, nilLogger)
	got = From(ctxNil)
	require.Equal(t, def, got, "ожидаем slog.Default() при nil-логгере")
}

func TestInto_ShadowParentLogger(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	parentL := newSilent()
	childL := newSilent()

	// parent имеет parentL.
	parent := Into(context.Background(), parentL)
	require.Equal(t, parentL, From(parent))

	// child «перекрывает» логгер родителя.
	child := Into(parent, childL)
	require.Equal(t, childL, From(child))

	// Родитель остался прежним.
	require.Equal(t, parentL, From(parent))
}
