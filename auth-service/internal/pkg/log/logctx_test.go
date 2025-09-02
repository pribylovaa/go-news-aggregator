// file: /mnt/data/logctx_test.go
package log

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Пакет тестов для internal/pkg/log (logctx.go).
//
// Покрытие:
//  - From без логгера в контексте -> возвращает slog.Default();
//  - Into/From round-trip с явным *slog.Logger;
//  - устойчивость к «мусорным» значениям и *slog.Logger(nil) в контексте;
//  - «перекрытие» логгера дочерним контекстом без влияния на родительский;
//  - сохранность прочих значений контекста (Into не трогает context.Value);
//  - сохранность отмены/дедлайна (Into не меняет Cancel/Deadline).
//
// Важно: тесты меняют slog.Default(), поэтому намеренно НЕ используют t.Parallel().

func newSilent() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestFrom_ReturnsDefault_WhenNoLoggerInContext —
// если логгер не положен в контекст, From возвращает текущий slog.Default().
func TestFrom_ReturnsDefault_WhenNoLoggerInContext(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	def := newSilent()
	slog.SetDefault(def)

	got := From(context.Background())
	require.Equal(t, def, got, "From должен вернуть slog.Default(), если в контексте ничего нет")
}

// TestIntoAndFrom_RoundTrip —
// Into кладёт логгер в контекст, From извлекает его 1:1.
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

// TestFrom_ReturnsDefault_WhenStoredValueIsWrongTypeOrNil —
// From устойчив к «мусорным» значениям по нашему ключу и к *slog.Logger(nil).
func TestFrom_ReturnsDefault_WhenStoredValueIsWrongTypeOrNil(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	def := newSilent()
	slog.SetDefault(def)

	// 1) в контексте лежит значение "не того типа" под тем же ключом.
	ctxWrong := context.WithValue(context.Background(), ctxKey{}, "not-a-logger")
	got := From(ctxWrong)
	require.Equal(t, def, got, "Ожидаем slog.Default() при неверном типе значения")

	// 2) в контексте лежит *slog.Logger == nil.
	var nilLogger *slog.Logger
	ctxNil := context.WithValue(context.Background(), ctxKey{}, nilLogger)
	got = From(ctxNil)
	require.Equal(t, def, got, "Ожидаем slog.Default() при nil-логгере")
}

// TestInto_ShadowParentLogger —
// дочерний контекст может «перекрыть» логгер родителя, не влияя на него.
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

	// parent остался прежним.
	require.Equal(t, parentL, From(parent))
}

// TestInto_PreservesContextValues —
// Into не трогает прочие значения в context.Value.
func TestInto_PreservesContextValues(t *testing.T) {
	type vk struct{}
	key := vk{}
	val := "v"

	base := context.WithValue(context.Background(), key, val)
	l := newSilent()

	ctx := Into(base, l)

	require.Equal(t, l, From(ctx))
	require.Equal(t, val, ctx.Value(key))
}

// TestInto_PreservesCancellationAndDeadline —
// Into не меняет отмену и дедлайн: Done/Err/Deadline сохраняются.
func TestInto_PreservesCancellationAndDeadline(t *testing.T) {
	// 1) Deadline сохраняется.
	parentDL, cancelDL := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelDL()

	l := newSilent()
	child := Into(parentDL, l)

	cdl, ok := child.Deadline()
	require.True(t, ok, "Дедлайн должен сохраниться")
	pdl, _ := parentDL.Deadline()
	require.WithinDuration(t, pdl, cdl, time.Millisecond)

	select {
	case <-child.Done():
		require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Ожидали дедлайн у дочернего контекста")
	}

	// 2) Отмена сохраняется.
	parentCancel, cancel := context.WithCancel(context.Background())
	child2 := Into(parentCancel, l)
	cancel()
	select {
	case <-child2.Done():
		require.ErrorIs(t, child2.Err(), context.Canceled)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ожидали отмену у дочернего контекста")
	}
}
