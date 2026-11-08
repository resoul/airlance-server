package ws

import (
	"context"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/transport/codec"
)

const (
	codeInvalid         int32 = 400
	codeUnauthenticated int32 = 401
	codeForbidden       int32 = 403
	codeNotFound        int32 = 404
	codeConflict        int32 = 409
	codeUnprocessable   int32 = 422
	codeTooManyRequests int32 = 429
	codeInternal        int32 = 500
	codeUnavailable     int32 = 503
)

type Profiles interface {
	GetOwn(ctx context.Context, principal identity.Principal) (profile.Profile, error)
	GetPublic(ctx context.Context, principal identity.Principal, subject string) (profile.Profile, error)
	UpdateOwn(ctx context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error)
	AddPhoto(ctx context.Context, principal identity.Principal, photoMediaID, videoMediaID string) (profile.Profile, error)
	RemovePhoto(ctx context.Context, principal identity.Principal, photoMediaID string) (profile.Profile, error)
}

func reply(request codec.Frame, kind codec.Kind, body []byte) codec.Frame {
	return codec.Frame{RequestID: request.RequestID, Kind: kind, Body: body}
}

func failure(request codec.Frame, code int32, message string) codec.Frame {
	return reply(request, codec.KindError, codec.EncodeError(code, message))
}
