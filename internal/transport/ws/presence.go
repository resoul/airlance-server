package ws

import (
	"context"
	"sync"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

const (
	typingEvery = 2 * time.Second
	typingLimit = 4096
)

type presence struct {
	mu    sync.Mutex
	links map[string]map[*link]struct{}
	last  map[string]time.Time
}

func newPresence() *presence {
	return &presence{links: map[string]map[*link]struct{}{}, last: map[string]time.Time{}}
}

func (p *presence) add(subject string, l *link) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.links[subject] == nil {
		p.links[subject] = map[*link]struct{}{}
	}

	p.links[subject][l] = struct{}{}
}

func (p *presence) remove(subject string, l *link) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.links[subject], l)

	if len(p.links[subject]) == 0 {
		delete(p.links, subject)
	}
}

func (p *presence) allow(key string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if now.Sub(p.last[key]) < typingEvery {
		return false
	}

	if len(p.last) >= typingLimit {
		for stale, at := range p.last {
			if now.Sub(at) > time.Minute {
				delete(p.last, stale)
			}
		}
	}

	p.last[key] = now

	return true
}

func (p *presence) targets(subjects []string) []*link {
	p.mu.Lock()
	defer p.mu.Unlock()

	var found []*link

	for _, subject := range subjects {
		for next := range p.links[subject] {
			found = append(found, next)
		}
	}

	return found
}

func (h *Handler) chatReadersGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, messageID, err := codec.DecodeChatReadersGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	readers, err := h.conversation.Readers(ctx, principal, id, messageID)

	return h.membersReply(ctx, request, readers, err)
}

func (h *Handler) typingSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, thread, err := codec.DecodeTypingSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	if !h.typing.allow(id+"|"+principal.Subject, time.Now()) {
		return reply(request, codec.KindChatsChanged, codec.EncodeChatsChanged())
	}

	who, audience, err := h.conversation.Typing(ctx, principal, id)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	body := codec.EncodeTypingEvent(id, thread, who.Subject, displayName(who))
	frame := codec.Frame{Kind: codec.KindTypingEvent, Body: body}

	for _, target := range h.typing.targets(audience) {
		go func() {
			_ = target.write(context.WithoutCancel(ctx), frame)
		}()
	}

	return reply(request, codec.KindChatsChanged, codec.EncodeChatsChanged())
}
