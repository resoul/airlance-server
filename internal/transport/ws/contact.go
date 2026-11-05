package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

type Contacts interface {
	List(ctx context.Context, principal identity.Principal) ([]contact.Contact, error)
	Add(ctx context.Context, principal identity.Principal, subject, first, last string) ([]contact.Contact, error)
	AddByPhone(ctx context.Context, principal identity.Principal, phone, first, last string) ([]contact.Contact, error)
	Remove(ctx context.Context, principal identity.Principal, subject string) ([]contact.Contact, error)
	Sync(ctx context.Context, principal identity.Principal, phones []string) ([]contact.Contact, error)
}

func (h *Handler) contactsRequest(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	contacts, err := h.runContacts(ctx, principal, request)
	if errors.Is(err, codec.ErrInvalid) {
		return failure(request, codeInvalid, "invalid body")
	}

	if err != nil {
		return h.contactFailure(ctx, request, err)
	}

	rows := make([]codec.ContactData, len(contacts))
	for index, next := range contacts {
		rows[index] = codec.ContactData(next)
	}

	return reply(request, codec.KindContactsData, codec.EncodeContactsData(rows))
}

func (h *Handler) runContacts(
	ctx context.Context,
	principal identity.Principal,
	request codec.Frame,
) ([]contact.Contact, error) {
	switch request.Kind {
	case codec.KindContactAdd:
		next, err := codec.DecodeContactAdd(request.Body)
		if err != nil {
			return nil, err
		}

		return h.contacts.Add(ctx, principal, next.Target, next.FirstName, next.LastName)
	case codec.KindContactAddPhone:
		next, err := codec.DecodeContactAddPhone(request.Body)
		if err != nil {
			return nil, err
		}

		return h.contacts.AddByPhone(ctx, principal, next.Target, next.FirstName, next.LastName)
	case codec.KindContactRemove:
		subject, err := codec.DecodeContactRemove(request.Body)
		if err != nil {
			return nil, err
		}

		return h.contacts.Remove(ctx, principal, subject)
	case codec.KindContactsSync:
		phones, err := codec.DecodeContactsSync(request.Body)
		if err != nil {
			return nil, err
		}

		return h.contacts.Sync(ctx, principal, phones)
	default:
		return h.contacts.List(ctx, principal)
	}
}

func (h *Handler) contactFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, contact.ErrNotFound):
		return failure(request, codeNotFound, "contact_not_found")
	case errors.Is(err, contact.ErrInvalidPeer):
		return failure(request, codeUnprocessable, "invalid_peer")
	case errors.Is(err, contact.ErrInvalidName):
		return failure(request, codeUnprocessable, "invalid_name")
	case errors.Is(err, contact.ErrInvalidPhone):
		return failure(request, codeUnprocessable, "invalid_phone")
	case errors.Is(err, contact.ErrTooManyPhones):
		return failure(request, codeUnprocessable, "too_many_phones")
	case errors.Is(err, contact.ErrTooManyTries):
		return failure(request, codeTooManyRequests, "too_many_lookups")
	default:
		h.logger.ErrorContext(ctx, "contact request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}
