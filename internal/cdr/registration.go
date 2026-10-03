package cdr

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
)

// RegistrationAtCall is called only after registration permission is verified.
func RegistrationAtCall(ctx context.Context, store *registration.Repository, tenant string, call Call) (any, error) {
	id := call.AnsweredExtension
	if id == "" {
		id = call.OriginatingExtension
	}
	if id == "" {
		return nil, nil
	}
	statuses, gaps, e := store.At(ctx, tenant, id, call.Start)
	if e != nil {
		return nil, e
	}
	return struct {
		Statuses []registration.Status `json:"statuses"`
		Gaps     []registration.Gap    `json:"gaps"`
	}{statuses, gaps}, nil
}
