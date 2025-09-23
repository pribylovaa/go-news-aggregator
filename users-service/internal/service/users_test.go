package service

// Тесты сервисного слоя users-service (internal/service/users.go).
//
//  Проверяем:
//  - валидацию входов;
//  - маппинг ошибок storage -> service (InvalidArgument / NotFound / AlreadyExists / Internal);
//  - корректность сборки ProfileUpdate при UpdateProfile (mask/указатели, trim, запрет пустого username);
//  - no-op update (без mask и без указателей);
//  - happy-path каждого метода.
//
// Подготовка окружения:
//   go test ./internal/service -v -race -count=1
//
// Примечание: моки сгенерированы в пакете /mocks (MockProfilesStorage, MockAvatarsStorage).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/users-service/mocks"
	"github.com/stretchr/testify/require"
)

func newServiceWithMocks(t *testing.T) (*Service, *mocks.MockProfilesStorage, *mocks.MockAvatarsStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mp := mocks.NewMockProfilesStorage(ctrl)
	ma := mocks.NewMockAvatarsStorage(ctrl)
	s := New(mp, ma, &config.Config{})
	return s, mp, ma, ctrl
}

// mustProfile — быстрый хелпер для сборки профиля.
func mustProfile(uid uuid.UUID, name string) *models.Profile {
	return &models.Profile{
		UserID:    uid,
		Username:  name,
		Age:       21,
		Country:   "LV",
		Gender:    models.GenderFemale,
		AvatarKey: "",
		AvatarURL: "",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// Валидация: userID == uuid.Nil -> ErrInvalidArgument.
func TestService_ProfileByID_InvalidArgument(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.ProfileByID(context.Background(), uuid.Nil)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrNotFoundProfile -> ErrNotFound.
func TestService_ProfileByID_NotFound(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().ProfileByID(gomock.Any(), uid).Return(nil, storage.ErrNotFoundProfile)

	_, err := s.ProfileByID(context.Background(), uid)
	require.ErrorIs(t, err, ErrNotFound)
}

// Happy-path: успешное чтение профиля.
func TestService_ProfileByID_OK(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "alice")
	mp.EXPECT().ProfileByID(gomock.Any(), uid).Return(want, nil)

	got, err := s.ProfileByID(context.Background(), uid)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: пустой userID, пустой username (после TrimSpace), неверный gender.
func TestService_CreateProfile_ValidationErrors(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.CreateProfile(context.Background(), CreateProfileInput{
		UserID: uuid.Nil, Username: "x", Gender: models.GenderMale,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = s.CreateProfile(context.Background(), CreateProfileInput{
		UserID: uuid.New(), Username: "   ", Gender: models.GenderMale,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = s.CreateProfile(context.Background(), CreateProfileInput{
		UserID: uuid.New(), Username: "bob", Gender: models.Gender(99),
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrAlreadyExists -> ErrAlreadyExists.
func TestService_CreateProfile_AlreadyExists(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	in := CreateProfileInput{UserID: uuid.New(), Username: "dup", Gender: models.GenderOther}
	mp.EXPECT().
		CreateProfile(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrAlreadyExists)

	_, err := s.CreateProfile(context.Background(), in)
	require.ErrorIs(t, err, ErrAlreadyExists)
}

// Маппинг: любая иная ошибка стораджа -> ErrInternal.
func TestService_CreateProfile_InternalError(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	in := CreateProfileInput{UserID: uuid.New(), Username: "ok", Gender: models.GenderMale}
	mp.EXPECT().
		CreateProfile(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("pg down"))

	_, err := s.CreateProfile(context.Background(), in)
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешное создание профиля, проверка нормализации полей.
func TestService_CreateProfile_OK(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	in := CreateProfileInput{UserID: uid, Username: "  carol  ", Age: 30, Country: "EE", Gender: models.GenderFemale}
	want := mustProfile(uid, "carol")
	want.Age = 30
	want.Country = "EE"

	mp.EXPECT().
		CreateProfile(gomock.Any(), gomock.AssignableToTypeOf(&models.Profile{})).
		DoAndReturn(func(_ context.Context, p *models.Profile) (*models.Profile, error) {
			require.Equal(t, uid, p.UserID)
			require.Equal(t, "carol", p.Username)
			require.EqualValues(t, 30, p.Age)
			require.Equal(t, "EE", p.Country)
			require.Equal(t, models.GenderFemale, p.Gender)
			return want, nil
		})

	got, err := s.CreateProfile(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: mask содержит неизвестное поле -> ErrInvalidArgument.
func TestService_UpdateProfile_InvalidMaskField(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uuid.New(), Mask: []string{"unknown"},
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Валидация: mask требует поле, но указатель nil -> ErrInvalidArgument.
func TestService_UpdateProfile_MaskRequiresValue(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Mask: []string{"username", "age"},
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Валидация: gender вне диапазона -> ErrInvalidArgument.
func TestService_UpdateProfile_GenderOutOfRange(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	g := models.Gender(99)
	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Gender: &g, Mask: []string{"gender"},
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Валидация: mask требует username, а значение после TrimSpace пустое -> ErrInvalidArgument.
func TestService_UpdateProfile_UsernameTrimEmpty(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	name := "   "
	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Username: &name, Mask: []string{"username"},
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// No-op update: без mask и без указателей — ожидаем вызов UpdateProfile с пустым ProfileUpdate.
func TestService_UpdateProfile_NoMask_NoPointers_NoOp(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "keanu")

	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, upd storage.ProfileUpdate) (*models.Profile, error) {
			require.Nil(t, upd.Username)
			require.Nil(t, upd.Age)
			require.Nil(t, upd.Country)
			require.Nil(t, upd.Gender)
			return want, nil
		})

	got, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid,
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Маска: очистка country до пустой строки допустима (в отличие от username).
func TestService_UpdateProfile_Mask_CountryClearOK(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	empty := ""
	want := mustProfile(uid, "john")
	want.Country = ""

	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, upd storage.ProfileUpdate) (*models.Profile, error) {
			require.NotNil(t, upd.Country)
			require.Equal(t, "", *upd.Country)
			return want, nil
		})

	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Country: &empty, Mask: []string{"country"},
	})
	require.NoError(t, err)
}

// Маппинг: storage.ErrNotFoundProfile -> ErrNotFound.
func TestService_UpdateProfile_NotFound(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	name := "new"
	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		Return(nil, storage.ErrNotFoundProfile)

	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Username: &name, Mask: []string{"username"},
	})
	require.ErrorIs(t, err, ErrNotFound)
}

// Маппинг: иная ошибка стораджа -> ErrInternal.
func TestService_UpdateProfile_InternalError(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	name := "boom"
	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		Return(nil, errors.New("pg down"))

	_, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Username: &name, Mask: []string{"username"},
	})
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: без mask — обновляются только заданные указатели.
func TestService_UpdateProfile_OK_NoMask_Partial(t *testing.T) {
	s, mp, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	name := "neo"
	age := uint32(33)
	want := mustProfile(uid, name)
	want.Age = 33

	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, upd storage.ProfileUpdate) (*models.Profile, error) {
			require.NotNil(t, upd.Username)
			require.Equal(t, "neo", *upd.Username)
			require.NotNil(t, upd.Age)
			require.EqualValues(t, 33, *upd.Age)
			require.Nil(t, upd.Country)
			require.Nil(t, upd.Gender)
			return want, nil
		})

	got, err := s.UpdateProfile(context.Background(), UpdateProfileInput{
		UserID: uid, Username: &name, Age: &age,
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: userID, contentType (TrimSpace), contentLength > 0.
func TestService_AvatarUploadURL_Validation(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uuid.Nil, ContentType: "image/png", ContentLength: 1,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uuid.New(), ContentType: "", ContentLength: 1,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uuid.New(), ContentType: "image/png", ContentLength: 0,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrInvalidArgument -> ErrInvalidArgument.
func TestService_AvatarUploadURL_StorageInvalidArgument(t *testing.T) {
	s, _, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	ma.EXPECT().
		AvatarUploadURL(gomock.Any(), uid, "image/png", int64(10)).
		Return(nil, storage.ErrInvalidArgument)

	_, err := s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uid, ContentType: "image/png", ContentLength: 10,
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: иная ошибка стораджа -> ErrInternal.
func TestService_AvatarUploadURL_InternalError(t *testing.T) {
	s, _, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	ma.EXPECT().
		AvatarUploadURL(gomock.Any(), uid, "image/png", int64(5)).
		Return(nil, errors.New("s3 unreachable"))

	_, err := s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uid, ContentType: "image/png", ContentLength: 5,
	})
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешная генерация presigned URL.
func TestService_AvatarUploadURL_OK(t *testing.T) {
	s, _, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	ui := &storage.UploadInfo{
		UploadURL: "http://u",
		AvatarKey: "avatars/" + uid.String() + "/x.png",
	}
	ma.EXPECT().
		AvatarUploadURL(gomock.Any(), uid, "image/png", int64(5)).
		Return(ui, nil)

	got, err := s.AvatarUploadURL(context.Background(), AvatarUploadURLInput{
		UserID: uid, ContentType: "image/png", ContentLength: 5,
	})
	require.NoError(t, err)
	require.Equal(t, ui, got)
}

// Валидация: пустые входы.
func TestService_ConfirmAvatarUpload_Validation(t *testing.T) {
	s, _, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{
		UserID: uuid.Nil, AvatarKey: "k",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{
		UserID: uuid.New(), AvatarKey: "   ",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг (avatars): ErrInvalidArgument/ErrNotFound -> ErrInvalidArgument/ErrNotFound.
func TestService_ConfirmAvatarUpload_AvatarsErrors(t *testing.T) {
	s, _, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return("", storage.ErrInvalidArgument)
	_, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.ErrorIs(t, err, ErrInvalidArgument)

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return("", storage.ErrNotFoundAvatar)
	_, err = s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.ErrorIs(t, err, ErrNotFound)
}

// Маппинг (avatars): иная ошибка -> ErrInternal.
func TestService_ConfirmAvatarUpload_AvatarsInternal(t *testing.T) {
	s, _, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return("", errors.New("s3 down"))
	_, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.ErrorIs(t, err, ErrInternal)
}

// Маппинг (profiles): ErrNotFoundProfile -> ErrNotFound.
func TestService_ConfirmAvatarUpload_ProfileNotFound(t *testing.T) {
	s, mp, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn/a.png"
	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(nil, storage.ErrNotFoundProfile)

	_, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.ErrorIs(t, err, ErrNotFound)
}

// Маппинг (profiles): иная ошибка -> ErrInternal.
func TestService_ConfirmAvatarUpload_ProfileInternal(t *testing.T) {
	s, mp, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn/a.png"
	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(nil, errors.New("pg down"))

	_, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешное подтверждение загрузки.
func TestService_ConfirmAvatarUpload_OK(t *testing.T) {
	s, mp, ma, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn/a.png"
	want := mustProfile(uid, "z")
	want.AvatarKey = key
	want.AvatarURL = public

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(want, nil)

	got, err := s.ConfirmAvatarUpload(context.Background(), ConfirmAvatarUploadInput{UserID: uid, AvatarKey: key})
	require.NoError(t, err)
	require.Equal(t, want, got)
}
