package grpc

// Тесты транспортного слоя (gRPC) для UsersService.
// Подход как в news-service:
//  - используем gomock для стораджей ниже сервиса;
//  - конструируем реальный service.Service поверх моков;
//  - проверяем маппинг ошибок в gRPC-коды, валидацию UUID/входов,
//    корректную сборку входов/маски для UpdateProfile,
//    и конвертацию доменной модели в protobuf (включая поля/enum/таймстемпы).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	usersv1 "github.com/pribylovaa/go-news-aggregator/users-service/gen/go/users"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/users-service/mocks"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// newServerWithMocks — хелпер сборки UsersServer с реальным сервисом поверх мок-хранилищ.
func newServerWithMocks(t *testing.T) (*UsersServer, *mocks.MockProfilesStorage, *mocks.MockAvatarsStorage, *gomock.Controller) {
	t.Helper()

	ctrl := gomock.NewController(t)
	mp := mocks.NewMockProfilesStorage(ctrl)
	ma := mocks.NewMockAvatarsStorage(ctrl)

	svc := service.New(mp, ma, &config.Config{})
	srv := NewUsersServer(svc)

	return srv, mp, ma, ctrl
}

// mustProfile — быстрый хелпер доменной модели (с воспроизводимыми таймстемпами).
func mustProfile(uid uuid.UUID, name string) *models.Profile {
	ts := time.Unix(1710000000, 0).UTC()
	return &models.Profile{
		UserID:    uid,
		Username:  name,
		Age:       21,
		Country:   "LV",
		Gender:    models.GenderFemale,
		AvatarKey: "avatars/" + uid.String() + "/a.png",
		AvatarURL: "http://cdn/a.png",
		CreatedAt: ts,
		UpdatedAt: ts.Add(time.Minute),
	}
}

func TestGRPC_ProfileByID_InvalidUUID(t *testing.T) {
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.ProfileByID(context.Background(), &usersv1.ProfileByIDRequest{UserId: "not-a-uuid"})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_ProfileByID_NotFound(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().ProfileByID(gomock.Any(), uid).Return(nil, storage.ErrNotFoundProfile)

	_, err := srv.ProfileByID(context.Background(), &usersv1.ProfileByIDRequest{UserId: uid.String()})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPC_ProfileByID_Internal(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().ProfileByID(gomock.Any(), uid).Return(nil, errors.New("db down"))

	_, err := srv.ProfileByID(context.Background(), &usersv1.ProfileByIDRequest{UserId: uid.String()})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPC_ProfileByID_OK(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "alice")
	mp.EXPECT().ProfileByID(gomock.Any(), uid).Return(want, nil)

	got, err := srv.ProfileByID(context.Background(), &usersv1.ProfileByIDRequest{UserId: uid.String()})
	require.NoError(t, err)
	require.Equal(t, uid.String(), got.GetUserId())
	require.Equal(t, "alice", got.GetUsername())
	require.EqualValues(t, 21, got.GetAge())
	require.Equal(t, "LV", got.GetCountry())
	require.Equal(t, usersv1.Gender_FEMALE, got.GetGender())
	require.Equal(t, want.AvatarKey, got.GetAvatarKey())
	require.Equal(t, want.AvatarURL, got.GetAvatarUrl())
	require.Equal(t, want.CreatedAt.Unix(), got.GetCreatedAt())
	require.Equal(t, want.UpdatedAt.Unix(), got.GetUpdatedAt())
}

func TestGRPC_CreateProfile_InvalidUUID(t *testing.T) {
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.CreateProfile(context.Background(), &usersv1.CreateProfileRequest{
		UserId: "bad-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_CreateProfile_ValidationError_FromService(t *testing.T) {
	// Пустой username -> ErrInvalidArgument на уровне сервиса.
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	_, err := srv.CreateProfile(context.Background(), &usersv1.CreateProfileRequest{
		UserId:   uid.String(),
		Username: "   ",
		Gender:   usersv1.Gender_FEMALE,
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_CreateProfile_AlreadyExists(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().
		CreateProfile(gomock.Any(), gomock.AssignableToTypeOf(&models.Profile{})).
		Return(nil, storage.ErrAlreadyExists)

	_, err := srv.CreateProfile(context.Background(), &usersv1.CreateProfileRequest{
		UserId:   uid.String(),
		Username: "bob",
		Age:      10,
		Country:  "EE",
		Gender:   usersv1.Gender_MALE,
	})
	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestGRPC_CreateProfile_Internal(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().
		CreateProfile(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("pg is down"))

	_, err := srv.CreateProfile(context.Background(), &usersv1.CreateProfileRequest{
		UserId:   uid.String(),
		Username: "c",
		Gender:   usersv1.Gender_OTHER,
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPC_CreateProfile_OK(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "carol")
	want.Age = 30
	want.Country = "EE"
	want.Gender = models.GenderFemale

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

	got, err := srv.CreateProfile(context.Background(), &usersv1.CreateProfileRequest{
		UserId:   uid.String(),
		Username: "  carol  ",
		Age:      30,
		Country:  "EE",
		Gender:   usersv1.Gender_FEMALE,
	})
	require.NoError(t, err)
	require.Equal(t, "carol", got.GetUsername())
	require.EqualValues(t, 30, got.GetAge())
	require.Equal(t, usersv1.Gender_FEMALE, got.GetGender())
}

func TestGRPC_UpdateProfile_InvalidUUID(t *testing.T) {
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.UpdateProfile(context.Background(), &usersv1.UpdateProfileRequest{
		UserId: "bad-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_UpdateProfile_WithMask_SetsOnlyMaskedFields(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "newname")
	want.Country = ""

	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, upd storage.ProfileUpdate) (*models.Profile, error) {
			require.NotNil(t, upd.Username)
			require.Equal(t, "newname", *upd.Username)
			require.NotNil(t, upd.Country)
			require.Equal(t, "", *upd.Country)
			require.Nil(t, upd.Age)
			require.Nil(t, upd.Gender)
			return want, nil
		})

	mask := &fieldmaskpb.FieldMask{Paths: []string{"username", "country"}}
	got, err := srv.UpdateProfile(context.Background(), &usersv1.UpdateProfileRequest{
		UserId:     uid.String(),
		Username:   "newname",
		Country:    "",
		UpdateMask: mask,
	})
	require.NoError(t, err)
	require.Equal(t, "newname", got.GetUsername())
	require.Equal(t, "", got.GetCountry())
}

func TestGRPC_UpdateProfile_NoMask_UsesNonZeroProto3Fields(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	want := mustProfile(uid, "neo")
	want.Age = 33

	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, upd storage.ProfileUpdate) (*models.Profile, error) {
			require.NotNil(t, upd.Username)
			require.Equal(t, "neo", *upd.Username)
			require.NotNil(t, upd.Age)
			require.EqualValues(t, 33, *upd.Age)
			// Пустой country без mask должен игнорироваться.
			require.Nil(t, upd.Country)
			// GENDER_UNSPECIFIED — игнор.
			require.Nil(t, upd.Gender)
			return want, nil
		})

	got, err := srv.UpdateProfile(context.Background(), &usersv1.UpdateProfileRequest{
		UserId:   uid.String(),
		Username: "neo",
		Age:      33,
		Country:  "",
		Gender:   usersv1.Gender_GENDER_UNSPECIFIED,
	})
	require.NoError(t, err)
	require.Equal(t, "neo", got.GetUsername())
	require.EqualValues(t, 33, got.GetAge())
}

func TestGRPC_UpdateProfile_NotFound(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		Return(nil, storage.ErrNotFoundProfile)

	mask := &fieldmaskpb.FieldMask{Paths: []string{"username"}}
	_, err := srv.UpdateProfile(context.Background(), &usersv1.UpdateProfileRequest{
		UserId:     uid.String(),
		Username:   "x",
		UpdateMask: mask,
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPC_UpdateProfile_Internal(t *testing.T) {
	srv, mp, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	mp.EXPECT().
		UpdateProfile(gomock.Any(), uid, gomock.Any()).
		Return(nil, errors.New("db down"))

	mask := &fieldmaskpb.FieldMask{Paths: []string{"age"}}
	_, err := srv.UpdateProfile(context.Background(), &usersv1.UpdateProfileRequest{
		UserId:     uid.String(),
		Age:        1,
		UpdateMask: mask,
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPC_AvatarUploadURL_InvalidUUID(t *testing.T) {
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.AvatarUploadURL(context.Background(), &usersv1.AvatarUploadURLRequest{
		UserId: "bad-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_AvatarUploadURL_Validation_FromService(t *testing.T) {
	// Пустой content_type -> ErrInvalidArgument на уровне сервиса.
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	_, err := srv.AvatarUploadURL(context.Background(), &usersv1.AvatarUploadURLRequest{
		UserId:        uid.String(),
		ContentType:   "",
		ContentLength: 10,
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_AvatarUploadURL_OK(t *testing.T) {
	srv, _, ma, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	ui := &storage.UploadInfo{
		UploadURL: "http://minio/presigned",
		AvatarKey: "avatars/" + uid.String() + "/x.png",
		Expires:   90 * time.Second,
		RequiredHeader: map[string]string{
			"Content-Type":   "image/png",
			"Content-Length": "5",
		},
	}

	ma.EXPECT().
		AvatarUploadURL(gomock.Any(), uid, "image/png", int64(5)).
		Return(ui, nil)

	got, err := srv.AvatarUploadURL(context.Background(), &usersv1.AvatarUploadURLRequest{
		UserId:        uid.String(),
		ContentType:   "image/png",
		ContentLength: 5,
	})
	require.NoError(t, err)
	require.Equal(t, "http://minio/presigned", got.GetUploadUrl())
	require.Equal(t, ui.AvatarKey, got.GetAvatarKey())
	require.EqualValues(t, 90, got.GetExpiresSeconds())
	require.Equal(t, ui.RequiredHeader, got.GetRequiredHeaders())
}

func TestGRPC_ConfirmAvatarUpload_InvalidUUID(t *testing.T) {
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.ConfirmAvatarUpload(context.Background(), &usersv1.ConfirmAvatarUploadRequest{
		UserId: "bad-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_ConfirmAvatarUpload_Validation_FromService(t *testing.T) {
	// Пустой avatar_key -> ErrInvalidArgument на уровне сервиса.
	srv, _, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	_, err := srv.ConfirmAvatarUpload(context.Background(), &usersv1.ConfirmAvatarUploadRequest{
		UserId:    uid.String(),
		AvatarKey: "   ",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_ConfirmAvatarUpload_ProfileNotFound(t *testing.T) {
	srv, mp, ma, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn.local/a.png"

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(nil, storage.ErrNotFoundProfile)

	_, err := srv.ConfirmAvatarUpload(context.Background(), &usersv1.ConfirmAvatarUploadRequest{
		UserId:    uid.String(),
		AvatarKey: key,
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPC_ConfirmAvatarUpload_Internal(t *testing.T) {
	srv, mp, ma, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn.local/a.png"

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(nil, errors.New("db down"))

	_, err := srv.ConfirmAvatarUpload(context.Background(), &usersv1.ConfirmAvatarUploadRequest{
		UserId:    uid.String(),
		AvatarKey: key,
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPC_ConfirmAvatarUpload_OK(t *testing.T) {
	srv, mp, ma, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	key := "avatars/" + uid.String() + "/a.png"
	public := "http://cdn.local/a.png"
	want := mustProfile(uid, "z")
	want.AvatarKey = key
	want.AvatarURL = public

	ma.EXPECT().CheckAvatarUpload(gomock.Any(), uid, key).Return(public, nil)
	mp.EXPECT().ConfirmAvatarUpload(gomock.Any(), uid, key, public).Return(want, nil)

	got, err := srv.ConfirmAvatarUpload(context.Background(), &usersv1.ConfirmAvatarUploadRequest{
		UserId:    uid.String(),
		AvatarKey: key,
	})
	require.NoError(t, err)
	require.Equal(t, key, got.GetAvatarKey())
	require.Equal(t, public, got.GetAvatarUrl())
	require.Equal(t, uid.String(), got.GetUserId())
}
