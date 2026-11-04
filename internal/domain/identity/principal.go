package identity

import (
	"errors"
	"slices"
	"time"
)

var ErrForbidden = errors.New("forbidden")

type Principal struct {
	Subject   string
	Scopes    []string
	ExpiresAt time.Time
}

func (p Principal) HasScope(scope string) bool {
	return slices.Contains(p.Scopes, scope)
}
