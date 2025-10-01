package errors

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToHTTP_BaseMapping(t *testing.T) {
	tcs := []struct {
		name       string
		in         error
		wantStatus int
		wantCode   string
	}{
		{"invalid_argument", status.Error(codes.InvalidArgument, "x"), http.StatusBadRequest, "invalid_argument"},
		{"not_found", status.Error(codes.NotFound, "x"), http.StatusNotFound, "not_found"},
		{"already_exists", status.Error(codes.AlreadyExists, "x"), http.StatusConflict, "already_exists"},
		{"failed_prec", status.Error(codes.FailedPrecondition, "x"), http.StatusPreconditionFailed, "failed_precondition"},
		{"unauth", status.Error(codes.Unauthenticated, "x"), http.StatusUnauthorized, "unauthenticated"},
		{"perm_denied", status.Error(codes.PermissionDenied, "x"), http.StatusForbidden, "permission_denied"},
		{"res_exhausted", status.Error(codes.ResourceExhausted, "x"), http.StatusTooManyRequests, "resource_exhausted"},
		{"canceled", status.Error(codes.Canceled, "x"), StatusClientClosedRequest, "canceled"},
		{"deadline", status.Error(codes.DeadlineExceeded, "x"), http.StatusGatewayTimeout, "deadline_exceeded"},
		{"unavailable", status.Error(codes.Unavailable, "x"), http.StatusServiceUnavailable, "unavailable"},
		{"unimplemented", status.Error(codes.Unimplemented, "x"), http.StatusNotImplemented, "unimplemented"},
		{"internal", status.Error(codes.Internal, "x"), http.StatusInternalServerError, "internal"},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, resp := ToHTTP(tc.in)
			require.Equal(t, tc.wantStatus, gotStatus)
			require.Equal(t, tc.wantCode, resp.Error.Code)
			require.NotEmpty(t, resp.Error.Message)
		})
	}
}

func TestToHTTP_NilError_Returns500Internal(t *testing.T) {
	gotStatus, resp := ToHTTP(nil)
	require.Equal(t, http.StatusInternalServerError, gotStatus)
	require.Equal(t, "internal", resp.Error.Code)
	require.Equal(t, "internal error", resp.Error.Message)
}
