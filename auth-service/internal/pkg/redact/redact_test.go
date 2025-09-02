package redact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Пакет unit-тестов для internal/pkg/redact.go.
//
// Покрытие (табличные тесты):
//   - Email: happy-path (ASCII), короткая локальная часть (≤2), отсутствие/множество '@',
//     сохранение домена (в т.ч. регистр и «плюс-тег»), пустые строки/части,
//     Unicode-локали (многобайтовые руны).
//   - Литералы Token/Password.

// TestEmail_Table — табличные тесты на редактирование e-mail.
// Проверяем все ветки: валидный адрес, короткая локальная часть,
// невалидный формат, Unicode и граничные случаи с пустыми частями.
func TestEmail_Table(t *testing.T) {
	t.Parallel()

	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "ASCII_local_gt_2", args: args{"foobar@example.com"}, want: "fo***@example.com"},
		{name: "ASCII_local_len_1", args: args{"a@ex.com"}, want: "***@ex.com"},
		{name: "ASCII_local_len_2", args: args{"ab@ex.com"}, want: "***@ex.com"},
		{name: "invalid_no_at", args: args{"no-at-here"}, want: "***"},
		{name: "invalid_multiple_at", args: args{"a@b@c"}, want: "***"},
		{name: "preserve_domain_case_and_content", args: args{"abc.def+tag@EXAMPLE.org"}, want: "ab***@EXAMPLE.org"},
		{name: "empty_string", args: args{""}, want: "***"},
		{name: "empty_domain_allowed_by_impl", args: args{"user@"}, want: "us***@"},
		{name: "unicode_local_gt_2_runes", args: args{"юзер@пример.рф"}, want: "юз***@пример.рф"},
		{name: "unicode_local_len_2_runes", args: args{"юз@домен"}, want: "***@домен"},
		{name: "empty_local_allowed_by_impl", args: args{"@domain"}, want: "***@domain"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Email(tt.args.s)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestLiterals_TokenAndPassword — литералы для токенов/паролей неизменны.
func TestLiterals_TokenAndPassword(t *testing.T) {
	t.Parallel()

	require.Equal(t, "[REDACTED_TOKEN]", Token())
	require.Equal(t, "[REDACTED_PASSWORD]", Password())
}
