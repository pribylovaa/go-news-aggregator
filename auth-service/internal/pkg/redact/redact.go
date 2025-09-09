// redact предоставляет утилиты безопасного редактирования чувствительных
// данных для логов (e-mail, токены, пароли). Цель — исключить утечки секретов,
// сохранив при этом полезный для отладки контекст (например, домен e-mail).
package redact

import "strings"

// Email маскирует e-mail для логирования.
//
// Правила:
//   - Строка должна содержать РОВНО один символ '@', иначе возвращается "***";
//   - Локальная часть (до '@') заменяется на первые два символа (по рунам) + "***";
//   - Если длина локальной части ≤ 2 символов — возвращается "***@<domain>";
//   - Доменная часть возвращается без изменений (сохраняется регистр/содержимое).
//
// Примеры:
//
//	"foobar@example.com"   -> "fo***@example.com"
//	"ab@ex.com"            -> "***@ex.com"
//	"user@"                -> "us***@"
//	"no-at"                -> "***"
//	"abc.def+tag@EXAMPLE"  -> "ab***@EXAMPLE"
func Email(s string) string {
	// ровно один '@' — иначе считаем e-mail невалидным и редактируем полностью.
	if strings.Count(s, "@") != 1 {
		return "***"
	}

	i := strings.IndexByte(s, '@')
	local, domain := s[:i], s[i+1:]

	lr := []rune(local)
	if len(lr) > 2 {
		local = string(lr[:2]) + "***"
	} else {
		local = "***"
	}

	return local + "@" + domain
}

// Token возвращает литерал-заглушку для токена в логах.
func Token() string { return "[REDACTED_TOKEN]" }

// Password возвращает литерал-заглушку для пароля в логах.
func Password() string { return "[REDACTED_PASSWORD]" }
