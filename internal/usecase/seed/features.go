package seed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/message"
)

const (
	featuresStart = 90 * time.Minute
	featuresStep  = 2 * time.Minute
	voiceBars     = 32
)

type vote struct {
	who     int
	options []int32
}

func (s *Service) writeFeatures(ctx context.Context, chats map[string]string, viewer string) error {
	s.Clock.Set(time.Now().Add(-featuresStart))

	steps := []func(context.Context, map[string]string) error{
		s.writeReplies,
		s.writePlaces,
		s.writeFiles,
		s.writePolls,
		s.writeChannelPosts,
		s.writeFormatting,
	}

	for _, step := range steps {
		if err := step(ctx, chats); err != nil {
			return err
		}
	}

	if viewer == "" {
		return nil
	}

	return s.writeViewerState(ctx, chats, viewer)
}

func (s *Service) post(
	ctx context.Context,
	who int,
	chatID, clientID string,
	outgoing message.Outgoing,
) (message.Message, error) {
	s.Clock.Set(s.Clock.Now().Add(featuresStep))

	sent, err := s.Messaging.SendContent(ctx, principalOf(subjectOf(who)), chatID, clientID, outgoing)
	if err != nil {
		return message.Message{}, fmt.Errorf("write %s: %w", clientID, err)
	}

	return sent, nil
}

func (s *Service) react(ctx context.Context, chatID string, id int64, reactions map[int]string) error {
	for who, emoji := range reactions {
		if _, err := s.Messaging.SetReaction(ctx, principalOf(subjectOf(who)), chatID, id, emoji); err != nil {
			return fmt.Errorf("react: %w", err)
		}
	}

	return nil
}

func (s *Service) writeReplies(ctx context.Context, chats map[string]string) error {
	chat := chats["weekend"]

	list, err := s.post(ctx, 2, chat, "seed-f-list", message.Outgoing{Text: "Что берём: мангал, уголь, мясо, овощи"})
	if err != nil {
		return err
	}

	reply, err := s.post(ctx, 1, chat, "seed-f-reply", message.Outgoing{
		Text:      "Мангал и уголь беру я",
		ReplyToID: list.ID,
		Entities:  []message.Entity{{Kind: message.EntityBold, Offset: 0, Length: 14}},
	})
	if err != nil {
		return err
	}

	if err := s.react(ctx, chat, list.ID, map[int]string{0: "👍", 3: "👍", 4: "❤️"}); err != nil {
		return err
	}

	if err := s.react(ctx, chat, reply.ID, map[int]string{2: "🔥"}); err != nil {
		return err
	}

	if _, err := s.Messaging.SetPin(ctx, principalOf(subjectOf(0)), chat, list.ID, true); err != nil {
		return fmt.Errorf("pin: %w", err)
	}

	typo, err := s.post(ctx, 3, chat, "seed-f-typo", message.Outgoing{Text: "Выежжаем в девять"})
	if err != nil {
		return err
	}

	_, err = s.Messaging.Edit(ctx, principalOf(subjectOf(3)), chat, typo.ID, "Выезжаем в девять", nil)
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	return nil
}

func (s *Service) writePlaces(ctx context.Context, chats map[string]string) error {
	chat := chats["weekend"]

	_, err := s.post(ctx, 3, chat, "seed-f-place", message.Outgoing{Attachments: []message.Attachment{{
		Kind: message.AttachmentLocation, Latitude: 50.4501, Longitude: 30.5234, Name: "Дача",
	}}})
	if err != nil {
		return err
	}

	_, err = s.post(ctx, 4, chat, "seed-f-contact", message.Outgoing{Attachments: []message.Attachment{{
		Kind: message.AttachmentContact, Name: "Борис Мельник", Phone: "+15550100001",
	}}})

	return err
}

func (s *Service) writeFiles(ctx context.Context, chats map[string]string) error {
	chat := chats["weekend"]
	data := tone()

	id, err := s.storeFile(ctx, 0, "seed-voice", data)
	if err != nil {
		return err
	}

	waveform := make([]byte, voiceBars)
	for index := range waveform {
		waveform[index] = byte(40 + (index*37)%200)
	}

	_, err = s.post(ctx, 0, chat, "seed-f-voice", message.Outgoing{Attachments: []message.Attachment{{
		Kind: message.AttachmentVoice, MediaID: id, Size: int64(len(data)),
		DurationMs: soundSeconds * 1000, Waveform: waveform,
	}}})
	if err != nil {
		return err
	}

	note := []byte("Мангал\nУголь\nМясо\nОвощи\n")

	file, err := s.storeFile(ctx, 2, "seed-list-file", note)
	if err != nil {
		return err
	}

	_, err = s.post(ctx, 2, chat, "seed-f-file", message.Outgoing{Attachments: []message.Attachment{{
		Kind: message.AttachmentFile, MediaID: file, Name: "Список.txt", Size: int64(len(note)),
	}}})

	return err
}

func (s *Service) writePolls(ctx context.Context, chats map[string]string) error {
	plans := []struct {
		who      int
		room     string
		clientID string
		poll     message.Poll
		votes    []vote
	}{
		{
			1, "weekend", "seed-f-poll",
			message.Poll{
				Question: "Когда выезжаем?",
				Options:  pollOptions("В восемь", "В девять", "В десять"),
			},
			[]vote{{2, []int32{1}}, {3, []int32{1}}, {4, []int32{2}}, {0, []int32{0}}},
		},
		{
			4, "books", "seed-f-quiz",
			message.Poll{
				Question: "Сколько рассказов в сборнике?", Options: pollOptions("Пять", "Семь", "Девять"),
				Quiz: true, Correct: 1, Explanation: "В сборнике семь рассказов",
			},
			[]vote{{0, []int32{0}}, {5, []int32{1}}},
		},
		{
			9, "news", "seed-f-news-poll",
			message.Poll{
				Question: "Что улучшить в первую очередь?", Options: pollOptions("Скорость", "Дизайн", "Звонки"),
				Anonymous: true,
			},
			[]vote{{0, []int32{0}}, {1, []int32{1}}, {2, []int32{1}}, {3, []int32{2}}},
		},
	}

	for _, plan := range plans {
		poll := plan.poll
		chat := chats[plan.room]

		sent, err := s.post(ctx, plan.who, chat, plan.clientID, message.Outgoing{Poll: &poll})
		if err != nil {
			return err
		}

		for _, next := range plan.votes {
			_, err := s.Messaging.Vote(ctx, principalOf(subjectOf(next.who)), chat, sent.ID, next.options)
			if err != nil && !errors.Is(err, message.ErrAlreadyVoted) {
				return fmt.Errorf("vote: %w", err)
			}
		}
	}

	return nil
}

func pollOptions(texts ...string) []message.PollOption {
	options := make([]message.PollOption, len(texts))
	for index, text := range texts {
		options[index] = message.PollOption{Text: text}
	}

	return options
}

func (s *Service) writeChannelPosts(ctx context.Context, chats map[string]string) error {
	chat := chats["news"]

	post, err := s.post(ctx, 9, chat, "seed-f-post", message.Outgoing{
		Text: "Обновление: в чатах появились опросы, реакции и ответы на сообщения",
	})
	if err != nil {
		return err
	}

	if err := s.react(ctx, chat, post.ID, map[int]string{0: "👍", 1: "🔥", 2: "👍", 3: "❤️"}); err != nil {
		return err
	}

	comments := []struct {
		who  int
		text string
	}{
		{0, "Наконец-то! Опросы давно ждали"},
		{3, "Реакции удобные, спасибо"},
		{6, "Нашла мелочь с отступами, отпишу в личку"},
	}

	for index, next := range comments {
		clientID := fmt.Sprintf("seed-f-comment-%d", index)

		_, err := s.post(ctx, next.who, chat, clientID, message.Outgoing{Text: next.text, ThreadID: post.ID})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) writeFormatting(ctx context.Context, chats map[string]string) error {
	chat := chats["team"]

	lines := []struct {
		who      int
		clientID string
		text     string
		entities []message.Entity
	}{
		{7, "seed-f-code", "Перед пушем запусти make check", []message.Entity{
			{Kind: message.EntityCode, Offset: 20, Length: 10},
		}},
		{1, "seed-f-spoiler", "К релизу готовим сюрприз: кофе всем", []message.Entity{
			{Kind: message.EntitySpoiler, Offset: 26, Length: 9},
		}},
		{6, "seed-f-style", "Важно: ломается поиск, было — работало", []message.Entity{
			{Kind: message.EntityBold, Offset: 0, Length: 6},
			{Kind: message.EntityStrikethrough, Offset: 32, Length: 7},
			{Kind: message.EntityItalic, Offset: 16, Length: 7},
		}},
		{3, "seed-f-quote", "Вчера обсуждали: сначала тесты, потом релиз", []message.Entity{
			{Kind: message.EntityQuote, Offset: 17, Length: 26},
		}},
	}

	for _, next := range lines {
		_, err := s.post(ctx, next.who, chat, next.clientID, message.Outgoing{Text: next.text, Entities: next.entities})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) writeViewerState(ctx context.Context, chats map[string]string, viewer string) error {
	principal := principalOf(viewer)

	if _, err := s.Messaging.SetDraft(ctx, principal, chats["weekend"], "Я еду, но позже"); err != nil {
		return fmt.Errorf("draft: %w", err)
	}

	kira := principalOf(subjectOf(archivedFrom))

	chat, err := s.Messaging.OpenDirect(ctx, kira, viewer)
	if err != nil {
		s.Print("Пропускаю архивный чат: " + err.Error())

		return nil
	}

	if _, err := s.Messaging.Send(ctx, kira, chat.ID, "seed-archived", "Это старый чат, он лежит в архиве"); err != nil {
		return fmt.Errorf("write archived: %w", err)
	}

	if _, err := s.Messaging.SetArchived(ctx, principal, chat.ID, true); err != nil {
		return fmt.Errorf("archive: %w", err)
	}

	return nil
}
