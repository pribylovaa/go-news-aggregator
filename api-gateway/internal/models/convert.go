package models

import (
	authv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/auth"
	commentsv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/comments"
	newsv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/news"
	usersv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/users"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (m AuthRegisterRequest) ToProto() *authv1.RegisterRequest {
	return &authv1.RegisterRequest{
		Email:    m.Email,
		Password: m.Password,
	}
}

func (m AuthLoginRequest) ToProto() *authv1.LoginRequest {
	return &authv1.LoginRequest{
		Email:    m.Email,
		Password: m.Password,
	}
}

func (m AuthRefreshRequest) ToProto() *authv1.RefreshTokenRequest {
	return &authv1.RefreshTokenRequest{
		RefreshToken: m.RefreshToken,
	}
}

func (m AuthRevokeRequest) ToProto() *authv1.RevokeTokenRequest {
	return &authv1.RevokeTokenRequest{
		RefreshToken: m.RefreshToken,
	}
}

func (m AuthValidateRequest) ToProto() *authv1.ValidateTokenRequest {
	return &authv1.ValidateTokenRequest{
		AccessToken: m.AccessToken,
	}
}

func AuthFromProto(a *authv1.AuthResponse) AuthResponse {
	if a == nil {
		return AuthResponse{}
	}

	return AuthResponse{
		UserID:          a.GetUserId(),
		AccessToken:     a.GetAccessToken(),
		RefreshToken:    a.GetRefreshToken(),
		AccessExpiresAt: a.GetAccessExpiresAt(),
	}
}

func AuthRevokeFromProto(r *authv1.RevokeTokenResponse) AuthRevokeResponse {
	if r == nil {
		return AuthRevokeResponse{}
	}

	return AuthRevokeResponse{
		Ok: r.GetOk(),
	}
}

func AuthValidateFromProto(v *authv1.ValidateTokenResponse) AuthValidateResponse {
	if v == nil {
		return AuthValidateResponse{}
	}

	return AuthValidateResponse{
		Valid:  v.GetValid(),
		UserID: v.GetUserId(),
		Email:  v.GetEmail(),
	}
}

func UserFromProto(u *usersv1.Profile) User {
	if u == nil {
		return User{}
	}

	return User{
		UserID:    u.GetUserId(),
		Username:  u.GetUsername(),
		Age:       u.GetAge(),
		AvatarURL: u.GetAvatarUrl(),
		AvatarKey: u.GetAvatarKey(),
		CreatedAt: u.GetCreatedAt(),
		UpdatedAt: u.GetUpdatedAt(),
		Country:   u.GetCountry(),
		Gender:    Gender(u.GetGender()),
	}
}

func (m UpdateUserRequest) ToProto() *usersv1.UpdateProfileRequest {
	req := &usersv1.UpdateProfileRequest{
		UserId:   m.UserID,
		Username: m.Username,
		Age:      m.Age,
		Country:  m.Country,
		Gender:   usersv1.Gender(m.Gender),
	}

	// Сформируем update_mask по реально переданным значениям.
	// Примечание: из-за omitempty на REST-слое здесь считаем "заданностью" для:
	// - строк: непустая строка
	// - чисел: > 0
	var paths []string
	if m.Username != "" {
		paths = append(paths, "username")
	}

	if m.Age > 0 {
		paths = append(paths, "age")
	}

	if m.Country != "" {
		paths = append(paths, "country")
	}

	if m.Gender != 0 {
		paths = append(paths, "gender")
	}

	if len(paths) > 0 {
		req.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
	}

	return req
}

func (m AvatarPresignRequest) ToProto() *usersv1.AvatarUploadURLRequest {
	return &usersv1.AvatarUploadURLRequest{
		UserId:        m.UserID,
		ContentType:   m.ContentType,
		ContentLength: m.ContentLength,
	}
}

func AvatarPresignFromProto(p *usersv1.AvatarUploadURLResponse) AvatarPresignResponse {
	if p == nil {
		return AvatarPresignResponse{}
	}

	// копируем map<string,string>
	hdrs := make(map[string]string, len(p.GetRequiredHeaders()))
	for k, v := range p.GetRequiredHeaders() {
		hdrs[k] = v
	}

	return AvatarPresignResponse{
		UploadURL:      p.GetUploadUrl(),
		AvatarKey:      p.GetAvatarKey(),
		ExpiresSeconds: p.GetExpiresSeconds(),
		RequiredHeader: hdrs,
	}
}

func (m AvatarConfirmRequest) ToProto() *usersv1.ConfirmAvatarUploadRequest {
	return &usersv1.ConfirmAvatarUploadRequest{
		UserId:    m.UserID,
		AvatarKey: m.AvatarKey,
	}
}

func (m NewsListRequest) ToProto() *newsv1.ListNewsRequest {
	return &newsv1.ListNewsRequest{
		Limit:     m.Limit,
		PageToken: m.PageToken,
	}
}

func (m NewsGetRequest) ToProto() *newsv1.NewsByIDRequest {
	return &newsv1.NewsByIDRequest{
		Id: m.ID,
	}
}

func NewsFromProto(n *newsv1.News) News {
	if n == nil {
		return News{}
	}

	return News{
		ID:               n.GetId(),
		Title:            n.GetTitle(),
		Category:         n.GetCategory(),
		ShortDescription: n.GetShortDescription(),
		LongDescription:  n.GetLongDescription(),
		Link:             n.GetLink(),
		ImageURL:         n.GetImageUrl(),
		PublishedAt:      n.GetPublishedAt(),
		FetchedAt:        n.GetFetchedAt(),
	}
}

func NewsListFromProto(r *newsv1.ListNewsResponse) NewsListResponse {
	out := NewsListResponse{
		NextPageToken: "",
	}

	if r == nil {
		return out
	}

	out.NextPageToken = r.GetNextPageToken()
	if items := r.GetItems(); len(items) > 0 {
		out.Items = make([]News, 0, len(items))
		for _, it := range items {
			out.Items = append(out.Items, NewsFromProto(it))
		}
	}

	return out
}

func NewsGetFromProto(r *newsv1.NewsByIDResponse) NewsGetResponse {
	if r == nil {
		return NewsGetResponse{}
	}

	var item *News
	if r.GetItem() != nil {
		n := NewsFromProto(r.GetItem())
		item = &n
	}

	return NewsGetResponse{Item: item}
}

func CommentFromProto(c *commentsv1.Comment) Comment {
	if c == nil {
		return Comment{}
	}

	return Comment{
		ID:           c.GetId(),
		NewsID:       c.GetNewsId(),
		ParentID:     c.GetParentId(),
		UserID:       c.GetUserId(),
		Username:     c.GetUsername(),
		Content:      c.GetContent(),
		Level:        c.GetLevel(),
		RepliesCount: c.GetRepliesCount(),
		IsDeleted:    c.GetIsDeleted(),
		CreatedAt:    c.GetCreatedAt(),
		UpdatedAt:    c.GetUpdatedAt(),
		ExpiresAt:    c.GetExpiresAt(),
	}
}

func (m CreateCommentRequest) ToProto() *commentsv1.CreateCommentRequest {
	return &commentsv1.CreateCommentRequest{
		NewsId:   m.NewsID,
		ParentId: m.ParentID,
		UserId:   m.UserID,
		Username: m.Username,
		Content:  m.Content,
	}
}

func CreateCommentFromProto(r *commentsv1.CreateCommentResponse) CreateCommentResponse {
	if r == nil {
		return CreateCommentResponse{}
	}

	var cm *Comment
	if r.GetComment() != nil {
		c := CommentFromProto(r.GetComment())
		cm = &c
	}

	return CreateCommentResponse{Comment: cm}
}

func (m GetCommentRequest) ToProto() *commentsv1.CommentByIDRequest {
	return &commentsv1.CommentByIDRequest{
		Id: m.ID,
	}
}

func GetCommentFromProto(r *commentsv1.CommentByIDResponse) GetCommentResponse {
	if r == nil {
		return GetCommentResponse{}
	}

	var cm *Comment
	if r.GetComment() != nil {
		c := CommentFromProto(r.GetComment())
		cm = &c
	}

	return GetCommentResponse{Comment: cm}
}

// Список корневых комментариев для новости.
func (m ListRootCommentsRequest) ToProto() *commentsv1.ListByNewsRequest {
	return &commentsv1.ListByNewsRequest{
		NewsId:    m.NewsID,
		PageSize:  m.PageSize,
		PageToken: m.PageToken,
	}
}

func ListRootCommentsFromProto(r *commentsv1.ListByNewsResponse) ListRootCommentsResponse {
	out := ListRootCommentsResponse{
		NextPageToken: "",
	}
	if r == nil {
		return out
	}

	out.NextPageToken = r.GetNextPageToken()
	if list := r.GetComments(); len(list) > 0 {
		out.Comments = make([]Comment, 0, len(list))
		for _, it := range list {
			out.Comments = append(out.Comments, CommentFromProto(it))
		}
	}
	return out
}

// Список ответов на конкретный комментарий.
func (m ListRepliesRequest) ToProto() *commentsv1.ListRepliesRequest {
	return &commentsv1.ListRepliesRequest{
		ParentId:  m.ParentID,
		PageSize:  m.PageSize,
		PageToken: m.PageToken,
	}
}

func ListRepliesFromProto(r *commentsv1.ListRepliesResponse) ListRepliesResponse {
	out := ListRepliesResponse{
		NextPageToken: "",
	}
	if r == nil {
		return out
	}

	out.NextPageToken = r.GetNextPageToken()
	if list := r.GetComments(); len(list) > 0 {
		out.Comments = make([]Comment, 0, len(list))
		for _, it := range list {
			out.Comments = append(out.Comments, CommentFromProto(it))
		}
	}

	return out
}
