// grpc содержит реализацию gRPC-эндпоинтов UsersService.
//
// Принципы:
//   - Контекст запроса прокидывается в сервис без потерь;
//   - Входные данные валидируются на уровне транспорта (например, UUID);
//   - Ошибки сервиса маппятся в коды gRPC:
//     ErrInvalidArgument -> codes.InvalidArgument;
//     ErrAlreadyExists   -> codes.AlreadyExists;
//     ErrNotFound        -> codes.NotFound;
//     иные               -> codes.Internal (единое безопасное сообщение).
package grpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	usersv1 "github.com/pribylovaa/go-news-aggregator/users-service/gen/go/users"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UsersServer struct {
	usersv1.UnimplementedUsersServiceServer
	service *service.Service
}

// NewUsersServer создаёт gRPC-сервер UsersService.
func NewUsersServer(svc *service.Service) *UsersServer {
	return &UsersServer{service: svc}
}

// ProfileByID возвращает профиль по идентификатору пользователя.
// Маппинг ошибок:
//   - неверный UUID -> InvalidArgument;
//   - ErrNotFound -> NotFound;
//   - прочее -> Internal (без раскрытия деталей).
func (s *UsersServer) ProfileByID(ctx context.Context, req *usersv1.ProfileByIDRequest) (*usersv1.Profile, error) {
	const op = "transport/grpc/users/ProfileByID"

	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	profile, err := s.service.ProfileByID(ctx, userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return toProtoProfile(*profile), nil
}

// CreateProfile создаёт новый профиль.
// Маппинг ошибок:
//   - неверный UUID -> InvalidArgument;
//   - ErrInvalidArgument -> InvalidArgument;
//   - ErrAlreadyExists -> AlreadyExists;
//   - прочее -> Internal.
func (s *UsersServer) CreateProfile(ctx context.Context, req *usersv1.CreateProfileRequest) (*usersv1.Profile, error) {
	const op = "transport/grpc/users/CreateProfile"

	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	profile, err := s.service.CreateProfile(ctx, service.CreateProfileInput{
		UserID:   userID,
		Username: req.GetUsername(),
		Age:      req.GetAge(),
		Country:  req.GetCountry(),
		Gender:   models.Gender(req.GetGender()),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrAlreadyExists):
			return nil, status.Errorf(codes.AlreadyExists, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return toProtoProfile(*profile), nil
}

// UpdateProfile выполняет частичное обновление профиля.
// Поддерживается field mask: paths=["username","age","country","gender"].
// Маппинг ошибок:
//   - неверный UUID -> InvalidArgument;
//   - ErrInvalidArgument -> InvalidArgument;
//   - ErrNotFound -> NotFound;
//   - прочее -> Internal.
//
// Правила передачи значений:
//   - При непустой mask значения берутся из полей запроса (включая пустые строки — для очистки).
//   - При пустой mask значения берутся только для «ненулевых» значений proto3 (string!= "", age!=0,
//     gender!=GENDER_UNSPECIFIED). Чтобы установить пустую строку — необходимо использовать mask.
func (s *UsersServer) UpdateProfile(ctx context.Context, req *usersv1.UpdateProfileRequest) (*usersv1.Profile, error) {
	const op = "transport/grpc/users/UpdateProfile"

	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	paths := req.GetUpdateMask().GetPaths()
	mask := make([]string, 0, len(paths))
	for _, p := range paths {
		mask = append(mask, strings.ToLower(strings.TrimSpace(p)))
	}

	in := service.UpdateProfileInput{
		UserID: userID,
		Mask:   mask,
	}

	useMask := len(mask) > 0

	// username.
	if useMask {
		for _, p := range mask {
			if p == "username" {
				v := req.GetUsername()
				in.Username = &v
				break
			}
		}
	} else if req.GetUsername() != "" {
		v := req.GetUsername()
		in.Username = &v
	}

	// age.
	if useMask {
		for _, p := range mask {
			if p == "age" {
				v := req.GetAge()
				in.Age = &v
				break
			}
		}
	} else if req.GetAge() != 0 {
		v := req.GetAge()
		in.Age = &v
	}

	// country (пустая строка допустима при наличии mask).
	if useMask {
		for _, p := range mask {
			if p == "country" {
				v := req.GetCountry()
				in.Country = &v
				break
			}
		}
	} else if req.GetCountry() != "" {
		v := req.GetCountry()
		in.Country = &v
	}

	// gender.
	if useMask {
		for _, p := range mask {
			if p == "gender" {
				g := models.Gender(req.GetGender())
				in.Gender = &g
				break
			}
		}
	} else if req.GetGender() != usersv1.Gender_GENDER_UNSPECIFIED {
		g := models.Gender(req.GetGender())
		in.Gender = &g
	}

	profile, err := s.service.UpdateProfile(ctx, in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return toProtoProfile(*profile), nil
}

// AvatarUploadURL выдаёт presigned PUT URL для загрузки аватара.
// Маппинг ошибок:
//   - неверный UUID -> InvalidArgument;
//   - ErrInvalidArgument -> InvalidArgument;
//   - прочее -> Internal.
func (s *UsersServer) AvatarUploadURL(ctx context.Context, req *usersv1.AvatarUploadURLRequest) (*usersv1.AvatarUploadURLResponse, error) {
	const op = "transport/grpc/users/AvatarUploadURL"

	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	response, err := s.service.AvatarUploadURL(ctx, service.AvatarUploadURLInput{
		UserID:        userID,
		ContentType:   req.GetContentType(),
		ContentLength: int64(req.GetContentLength()),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return &usersv1.AvatarUploadURLResponse{
		UploadUrl:       response.UploadURL,
		AvatarKey:       response.AvatarKey,
		ExpiresSeconds:  uint32(response.Expires / time.Second),
		RequiredHeaders: response.RequiredHeader,
	}, nil
}

// ConfirmAvatarUpload подтверждает загрузку аватара (валидирует объект и фиксирует в профиле).
// Маппинг ошибок:
//   - неверный UUID -> InvalidArgument;
//   - ErrInvalidArgument -> InvalidArgument;
//   - ErrNotFound -> NotFound;
//   - прочее -> Internal.
func (s *UsersServer) ConfirmAvatarUpload(ctx context.Context, req *usersv1.ConfirmAvatarUploadRequest) (*usersv1.Profile, error) {
	const op = "transport/grpc/users/ConfirmAvatarUpload"

	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	profile, err := s.service.ConfirmAvatarUpload(ctx, service.ConfirmAvatarUploadInput{
		UserID:    userID,
		AvatarKey: req.GetAvatarKey(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return toProtoProfile(*profile), nil
}

// toProtoProfile конвертирует доменную модель Profile в protobuf-представление.
func toProtoProfile(p models.Profile) *usersv1.Profile {
	return &usersv1.Profile{
		UserId:    p.UserID.String(),
		Username:  p.Username,
		Age:       p.Age,
		AvatarUrl: p.AvatarURL,
		AvatarKey: p.AvatarKey,
		CreatedAt: p.CreatedAt.Unix(),
		UpdatedAt: p.UpdatedAt.Unix(),
		Country:   p.Country,
		Gender:    toProtoGender(p.Gender),
	}
}

// toProtoGender конвертирует доменную модель Gender в protobuf-представление.
func toProtoGender(g models.Gender) usersv1.Gender {
	switch g {
	case models.GenderMale:
		return usersv1.Gender_MALE
	case models.GenderFemale:
		return usersv1.Gender_FEMALE
	case models.GenderOther:
		return usersv1.Gender_OTHER
	default:
		return usersv1.Gender_GENDER_UNSPECIFIED
	}
}
