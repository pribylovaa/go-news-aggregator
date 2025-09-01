package models

import "time"

// TokenPair — пара токенов, выдаваемая при аутентификации/регистрации.
//
// Описание:
//   - AccessToken — короткоживущий JWT для доступа к API;
//   - RefreshToken — случайный секрет, который клиент хранит и предъявляет
//     для выпуска новой пары токенов; на сервере хранится только его хэш;
//   - AccessExpiresAt — момент истечения access-токена (UTC).
type TokenPair struct {
	// AccessToken — JWT для авторизации запросов.
	AccessToken string
	// RefreshToken — случайный секрет для обновления пары.
	RefreshToken string
	// AccessExpiresAt — время истечения действия access-токена (UTC).
	AccessExpiresAt time.Time
}
