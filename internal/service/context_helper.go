package service

import "github.com/go-tangra/go-tangra-common/grpcx"

// Re-export the metadata helpers used by the service layer for clarity and
// so tests can stub them in one place.
var (
	getTenantIDFromContext = grpcx.GetTenantIDFromContext
	getUserIDAsUint32      = grpcx.GetUserIDAsUint32
	getUsernameFromContext = grpcx.GetUsernameFromContext
	isPlatformAdmin        = grpcx.IsPlatformAdmin
)
