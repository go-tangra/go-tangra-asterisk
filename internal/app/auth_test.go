package app

import (
	"context"
	authv1 "github.com/go-tangra/go-tangra-auth/sdk/v4/api/proto/auth/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type authzClient struct {
	authv1.AuthorizationClient
	res *authv1.CheckResponse
	err error
}

func (a authzClient) Check(context.Context, *authv1.CheckRequest, ...grpc.CallOption) (*authv1.CheckResponse, error) {
	return a.res, a.err
}

func TestAuthPermsOutageIsNotADenial(t *testing.T) {
	for _, tc := range []struct {
		name    string
		client  authzClient
		allowed bool
		err     bool
	}{
		{"allowed", authzClient{res: &authv1.CheckResponse{Allowed: true}}, true, false},
		{"denied", authzClient{res: &authv1.CheckResponse{}}, false, false},
		{"permission denied", authzClient{err: status.Error(codes.PermissionDenied, "no")}, false, false},
		{"unavailable", authzClient{err: status.Error(codes.Unavailable, "down")}, false, true},
		{"timeout", authzClient{err: status.Error(codes.DeadlineExceeded, "slow")}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, e := AuthPerms{Client: tc.client}.Has(context.Background(), "t", "u", "calls:read")
			if ok != tc.allowed || (e != nil) != tc.err {
				t.Fatal(ok, e)
			}
		})
	}
}
