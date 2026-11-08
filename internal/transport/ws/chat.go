package ws

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
	usecase "go.airlance.org/server/internal/usecase/message"
)

type Conversation interface {
	Send(ctx context.Context, principal identity.Principal, chatID, clientID, text string) (message.Message, error)
	SendContent(
		ctx context.Context,
		principal identity.Principal,
		chatID, clientID string,
		outgoing message.Outgoing,
	) (message.Message, error)
	Edit(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		text string,
		entities []message.Entity,
	) (message.Message, error)
	Delete(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		ids []int64,
		forEveryone bool,
	) ([]int64, error)
	History(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		before int64,
		limit int,
	) ([]message.Message, error)
	Thread(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		thread, before int64,
		limit int,
	) ([]message.Message, error)
	Pins(ctx context.Context, principal identity.Principal, chatID string) ([]int64, error)
	SetPin(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		isPinned bool,
	) ([]int64, error)
	SetRead(ctx context.Context, principal identity.Principal, chatID string, upTo int64) (message.ReadState, error)
	SetDelivered(ctx context.Context, principal identity.Principal, chatID string, upTo int64) (message.ReadState, error)
	PostStats(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		ids []int64,
		markViewed bool,
	) ([]message.PostStat, error)
	Readers(ctx context.Context, principal identity.Principal, chatID string, messageID int64) ([]message.Member, error)
	Typing(ctx context.Context, principal identity.Principal, chatID string) (message.Peer, []string, error)
}

type ChatList interface {
	OpenDirect(ctx context.Context, principal identity.Principal, peer string) (message.Chat, error)
	Chats(ctx context.Context, principal identity.Principal, before int64) ([]message.Chat, error)
	Create(ctx context.Context, principal identity.Principal, next usecase.NewChat) (message.Chat, error)
	Join(ctx context.Context, principal identity.Principal, code string) (message.Chat, error)
	Leave(ctx context.Context, principal identity.Principal, chatID string) error
	SetPinned(ctx context.Context, principal identity.Principal, chatID string, isPinned bool) (message.Chat, error)
	SetMuted(ctx context.Context, principal identity.Principal, chatID string, isMuted bool) (message.Chat, error)
	SetArchived(ctx context.Context, principal identity.Principal, chatID string, isArchived bool) (message.Chat, error)
	SetDraft(ctx context.Context, principal identity.Principal, chatID, text string) (message.DraftState, error)
	ResetInvite(ctx context.Context, principal identity.Principal, chatID string) (message.Chat, error)
}

type ChatAdmin interface {
	Members(ctx context.Context, principal identity.Principal, chatID string) ([]message.Member, error)
	AddMember(
		ctx context.Context,
		principal identity.Principal,
		chatID, subject string,
		role message.Role,
		rights message.Rights,
	) ([]message.Member, error)
	RemoveMember(
		ctx context.Context,
		principal identity.Principal,
		chatID, subject string,
		ban bool,
	) ([]message.Member, error)
	SetMemberRights(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		rights message.Rights,
	) (message.Chat, error)
	SetInfo(ctx context.Context, principal identity.Principal, chatID string, info message.ChatInfo) (message.Chat, error)
	SetRules(ctx context.Context, principal identity.Principal, chatID string, rules message.ChatRules) (message.Chat, error)
	Bans(ctx context.Context, principal identity.Principal, chatID string) ([]message.Member, error)
	Unban(ctx context.Context, principal identity.Principal, chatID, subject string) ([]message.Member, error)
	ChatLog(ctx context.Context, principal identity.Principal, chatID string) ([]message.LogEntry, error)
}

type Interactions interface {
	SetReaction(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		emoji string,
	) (message.Message, error)
	Vote(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		options []int32,
	) (message.Message, error)
	ClosePoll(ctx context.Context, principal identity.Principal, chatID string, id int64) (message.Message, error)
}

type Messaging interface {
	Conversation
	ChatList
	ChatAdmin
	Interactions
}

func (h *Handler) chatOpenDirect(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	peer, err := codec.DecodeChatOpenDirect(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.OpenDirect(ctx, principal, peer)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) messageSend(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	send, err := codec.DecodeMessageSend(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	outgoing := message.Outgoing{
		Text: send.Text, ForwardName: send.ForwardName, ReplyToID: send.ReplyToID, Entities: entitiesOf(send.Entities),
		ThreadID: send.ThreadID, Poll: pollDraft(send.Poll),
	}
	for _, next := range send.Attachments {
		outgoing.Attachments = append(outgoing.Attachments, message.Attachment{
			Kind:         message.AttachmentKind(next.Kind),
			MediaID:      next.MediaID,
			ThumbMediaID: next.ThumbMediaID,
			Name:         next.Name,
			Size:         next.Size,
			Width:        next.Width,
			Height:       next.Height,
			DurationMs:   next.DurationMs,
			Waveform:     next.Waveform,
			Title:        next.Title,
			Performer:    next.Performer,
			Latitude:     next.Latitude,
			Longitude:    next.Longitude,
			Phone:        next.Phone,
		})
	}

	stored, err := h.conversation.SendContent(ctx, principal, send.ChatID, send.ClientID, outgoing)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(stored))
}

func (h *Handler) messageEdit(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	edit, err := codec.DecodeMessageEdit(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	stored, err := h.conversation.Edit(ctx, principal, edit.ChatID, edit.MessageID, edit.Text, entitiesOf(edit.Entities))
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(stored))
}

func (h *Handler) messageDelete(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	remove, err := codec.DecodeMessageDelete(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	removed, err := h.conversation.Delete(ctx, principal, remove.ChatID, remove.MessageIDs, remove.ForEveryone)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessagesDeleted, codec.EncodeMessagesDeleted(remove.ChatID, removed))
}

func (h *Handler) messagePinSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	set, err := codec.DecodeMessagePinSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	pins, err := h.conversation.SetPin(ctx, principal, set.ChatID, set.MessageID, set.IsPinned)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindPinsData, codec.EncodePinsData(set.ChatID, pins))
}

func (h *Handler) pinsGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	chatID, err := codec.DecodePinsGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	pins, err := h.conversation.Pins(ctx, principal, chatID)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindPinsData, codec.EncodePinsData(chatID, pins))
}

func (h *Handler) postStatsGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	get, err := codec.DecodePostStatsGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	stats, err := h.conversation.PostStats(ctx, principal, get.ChatID, get.MessageIDs, get.MarkViewed)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	items := make([]codec.PostStat, len(stats))

	for index, next := range stats {
		items[index] = codec.PostStat{MessageID: next.MessageID, Views: next.Views, Comments: next.Comments}

		for _, person := range next.Recent {
			items[index].Recent = append(items[index].Recent, codec.PostCommenter(person))
		}
	}

	return reply(request, codec.KindPostStatsData, codec.EncodePostStatsData(get.ChatID, items))
}

func (h *Handler) chatReadSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	chatID, upTo, err := codec.DecodeChatReadSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	state, err := h.conversation.SetRead(ctx, principal, chatID, upTo)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindReadData, readBody(state))
}

func (h *Handler) chatDeliveredSet(
	ctx context.Context,
	principal identity.Principal,
	request codec.Frame,
) codec.Frame {
	chatID, upTo, err := codec.DecodeChatDeliveredSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	state, err := h.conversation.SetDelivered(ctx, principal, chatID, upTo)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindReadData, readBody(state))
}

func (h *Handler) reactionSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	set, err := codec.DecodeReactionSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	reacted, err := h.interactions.SetReaction(ctx, principal, set.ChatID, set.MessageID, set.Emoji)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(reacted))
}

func (h *Handler) pollVote(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	vote, err := codec.DecodePollVote(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	voted, err := h.interactions.Vote(ctx, principal, vote.ChatID, vote.MessageID, vote.Options)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(voted))
}

func (h *Handler) pollClose(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	chatID, id, err := codec.DecodePollClose(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	closed, err := h.interactions.ClosePoll(ctx, principal, chatID, id)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(closed))
}

func pollDraft(poll *codec.Poll) *message.Poll {
	if poll == nil {
		return nil
	}

	draft := &message.Poll{
		Question: poll.Question, Anonymous: poll.Anonymous, Multiple: poll.Multiple, Quiz: poll.IsQuiz,
		Correct: poll.CorrectOption, Explanation: poll.Explanation,
	}

	if poll.CloseAtMs != 0 {
		draft.CloseAt = time.UnixMilli(poll.CloseAtMs)
	}

	for _, next := range poll.Options {
		draft.Options = append(draft.Options, message.PollOption{Text: next.Text})
	}

	return draft
}

func pollRows(poll *message.Poll) *codec.Poll {
	if poll == nil {
		return nil
	}

	rows := &codec.Poll{
		Question: poll.Question, Anonymous: poll.Anonymous, Multiple: poll.Multiple, IsQuiz: poll.Quiz,
		CorrectOption: poll.Correct, Explanation: poll.Explanation, IsClosed: poll.IsClosed,
		TotalVoters: poll.TotalVoters, MyChoices: poll.Mine,
	}

	if !poll.CloseAt.IsZero() {
		rows.CloseAtMs = poll.CloseAt.UnixMilli()
	}

	for _, next := range poll.Options {
		option := codec.PollOption{Text: next.Text, Votes: next.Votes}
		for _, voter := range next.Voters {
			option.Voters = append(option.Voters, codec.PostCommenter(voter))
		}

		rows.Options = append(rows.Options, option)
	}

	return rows
}

func (h *Handler) chatArchiveSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	chatID, isArchived, err := codec.DecodeChatArchiveSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.SetArchived(ctx, principal, chatID, isArchived)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) draftSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	chatID, text, err := codec.DecodeDraftSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	state, err := h.chatList.SetDraft(ctx, principal, chatID, text)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindDraftData, draftBody(state))
}

func draftBody(state message.DraftState) []byte {
	return codec.EncodeDraftData(codec.DraftData{ChatID: state.ChatID, Text: state.Text, UpdatedAtMs: state.UpdatedAtMs})
}

func readBody(state message.ReadState) []byte {
	return codec.EncodeReadData(codec.ReadData{
		ChatID: state.ChatID, MyReadID: state.MyReadID, PeerReadID: state.PeerReadID, Unread: state.Unread,
		PeerDeliveredID: state.PeerDeliveredID,
	})
}

func reactionCounts(counts []message.ReactionCount) []codec.ReactionCount {
	rows := make([]codec.ReactionCount, len(counts))
	for index, next := range counts {
		rows[index] = codec.ReactionCount(next)
	}

	return rows
}

func entitiesOf(rows []codec.Entity) []message.Entity {
	entities := make([]message.Entity, len(rows))
	for index, next := range rows {
		entities[index] = message.Entity{
			Kind: message.EntityKind(next.Kind), Offset: next.Offset, Length: next.Length, UserID: next.UserID,
		}
	}

	return entities
}

func entityRows(entities []message.Entity) []codec.Entity {
	rows := make([]codec.Entity, len(entities))
	for index, next := range entities {
		rows[index] = codec.Entity{Kind: int32(next.Kind), Offset: next.Offset, Length: next.Length, UserID: next.UserID}
	}

	return rows
}

func (h *Handler) historyGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	get, err := codec.DecodeHistoryGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	messages, err := h.conversation.Thread(ctx, principal, get.ChatID, get.ThreadID, get.BeforeID, int(get.Limit))
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.MessageData, len(messages))
	for index, next := range messages {
		rows[index] = messageData(next)
	}

	return reply(request, codec.KindHistoryData, codec.EncodeHistoryData(rows))
}

func (h *Handler) chatsGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	before, err := codec.DecodeChatsGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chats, err := h.chatList.Chats(ctx, principal, before)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.ChatData, len(chats))
	for index, next := range chats {
		rows[index] = chatData(next)
	}

	return reply(request, codec.KindChatsData, codec.EncodeChatsData(rows))
}

func (h *Handler) chatFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, message.ErrChatNotFound):
		return failure(request, codeNotFound, "chat_not_found")
	case errors.Is(err, message.ErrPeerNotFound):
		return failure(request, codeNotFound, "peer_not_found")
	case errors.Is(err, message.ErrBlocked):
		return failure(request, codeForbidden, "blocked")
	case errors.Is(err, message.ErrRestricted):
		return failure(request, codeForbidden, "privacy_restricted")
	case errors.Is(err, message.ErrNotAllowed):
		return failure(request, codeForbidden, "not_allowed")
	case errors.Is(err, message.ErrTooManyPinned):
		return failure(request, codeConflict, "too_many_pinned")
	case errors.Is(err, message.ErrLinkTaken):
		return failure(request, codeConflict, "link_taken")
	case errors.Is(err, message.ErrInvalidTitle):
		return failure(request, codeUnprocessable, "invalid_title")
	case errors.Is(err, message.ErrInvalidAbout):
		return failure(request, codeUnprocessable, "invalid_about")
	case errors.Is(err, message.ErrInvalidLink):
		return failure(request, codeUnprocessable, "invalid_link")
	case errors.Is(err, message.ErrInvalidKind):
		return failure(request, codeUnprocessable, "invalid_kind")
	case errors.Is(err, message.ErrInvalidPeer):
		return failure(request, codeUnprocessable, "invalid_peer")
	case errors.Is(err, message.ErrInvalidAttachment):
		return failure(request, codeUnprocessable, "invalid_attachment")
	case errors.Is(err, message.ErrMessageMissing):
		return failure(request, codeNotFound, "message_not_found")
	case errors.Is(err, message.ErrEditExpired):
		return failure(request, codeForbidden, "edit_expired")
	case errors.Is(err, message.ErrNothingToEdit):
		return failure(request, codeForbidden, "not_editable")
	case errors.Is(err, message.ErrInvalidPoll):
		return failure(request, codeUnprocessable, "invalid_poll")
	case errors.Is(err, message.ErrInvalidVote):
		return failure(request, codeUnprocessable, "invalid_vote")
	case errors.Is(err, message.ErrPollClosed):
		return failure(request, codeConflict, "poll_closed")
	case errors.Is(err, message.ErrAlreadyVoted):
		return failure(request, codeConflict, "already_voted")
	case errors.Is(err, message.ErrInvalidReaction):
		return failure(request, codeUnprocessable, "invalid_reaction")
	case errors.Is(err, message.ErrInvalidRead):
		return failure(request, codeUnprocessable, "invalid_read")
	case errors.Is(err, message.ErrInvalidThread):
		return failure(request, codeUnprocessable, "invalid_thread")
	case errors.Is(err, message.ErrInvalidStats):
		return failure(request, codeUnprocessable, "invalid_stats")
	case errors.Is(err, message.ErrTooManyPins):
		return failure(request, codeConflict, "too_many_pins")
	case errors.Is(err, message.ErrInvalidReply):
		return failure(request, codeUnprocessable, "invalid_reply")
	case errors.Is(err, message.ErrInvalidEntity):
		return failure(request, codeUnprocessable, "invalid_entity")
	case errors.Is(err, message.ErrInvalidDelete):
		return failure(request, codeUnprocessable, "invalid_delete")
	case errors.Is(err, message.ErrInvalidText):
		return failure(request, codeUnprocessable, "invalid_text")
	case errors.Is(err, message.ErrInvalidClientID):
		return failure(request, codeUnprocessable, "invalid_client_id")
	default:
		h.logger.ErrorContext(ctx, "chat request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func messageData(next message.Message) codec.MessageData {
	attachments := make([]codec.Attachment, len(next.Attachments))
	for index, item := range next.Attachments {
		attachments[index] = codec.Attachment{
			Kind:         int32(item.Kind),
			MediaID:      item.MediaID,
			ThumbMediaID: item.ThumbMediaID,
			Name:         item.Name,
			Size:         item.Size,
			Width:        item.Width,
			Height:       item.Height,
			DurationMs:   item.DurationMs,
			Waveform:     item.Waveform,
			Title:        item.Title,
			Performer:    item.Performer,
			Latitude:     item.Latitude,
			Longitude:    item.Longitude,
			Phone:        item.Phone,
		}
	}

	var edited int64
	if !next.EditedAt.IsZero() {
		edited = next.EditedAt.UnixMilli()
	}

	return codec.MessageData{
		ReplyToID:    next.ReplyToID,
		ThreadID:     next.ThreadID,
		Reactions:    reactionCounts(next.Reactions),
		MyReaction:   next.MyReaction,
		Poll:         pollRows(next.Poll),
		Entities:     entityRows(next.Entities),
		EditedAtMs:   edited,
		ForwardName:  next.ForwardName,
		Attachments:  attachments,
		ChatID:       next.ChatID,
		MessageID:    next.ID,
		Sender:       next.Sender,
		ClientID:     next.ClientID,
		Text:         next.Text,
		SentAtUnixMs: next.SentAt.UnixMilli(),
		FirstName:    next.Author.FirstName,
		LastName:     next.Author.LastName,
		PhotoMediaID: next.Author.PhotoMediaID,
		VideoMediaID: next.Author.VideoMediaID,
		AgentName:    next.AgentName,
	}
}

func messageBody(next message.Message) []byte {
	return codec.EncodeMessageData(messageData(next))
}

func chatData(chat message.Chat) codec.ChatData {
	data := codec.ChatData{
		ChatID:           chat.ID,
		PeerSubject:      chat.Peer.Subject,
		PeerFirstName:    chat.Peer.FirstName,
		PeerLastName:     chat.Peer.LastName,
		PeerUsername:     chat.Peer.Username,
		PeerPhotoMediaID: chat.Peer.PhotoMediaID,
		PeerVideoMediaID: chat.Peer.VideoMediaID,
		Kind:             int32(chat.Kind),
		Title:            chat.Title,
		About:            chat.About,
		Link:             chat.Link,
		InviteToken:      chat.InviteToken,
		MemberCount:      int32(chat.MemberCount),
		Role:             int32(chat.Role),
		IsMember:         chat.IsMember,
		IsPinned:         chat.IsPinned,
		IsMuted:          chat.IsMuted,
		MyReadID:         chat.MyReadID,
		PeerReadID:       chat.PeerReadID,
		Unread:           chat.Unread,
		IsArchived:       chat.IsArchived,
		Draft:            chat.Draft,
		DraftAtMs:        chat.DraftAtMs,
		PeerDeliveredID:  chat.PeerDeliveredID,
		MemberRights:     int64(chat.MemberRights),
		MyRights:         int64(chat.MyRights()),
		Rules: codec.ChatRules{
			NoCopy:        chat.Rules.NoCopy,
			Signs:         chat.Rules.Signs,
			ReactionsMode: chat.Rules.ReactionsMode,
			Reactions:     chat.Rules.Reactions,
			PerPost:       chat.Rules.PerPost,
		},
	}

	if chat.Last != nil {
		last := messageData(*chat.Last)
		data.Last = &last
	}

	return data
}
