package seed

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/message"
)

const (
	soundRate    = 8000
	soundSeconds = 3
	soundPitch   = 440.0
	soundVolume  = 8000.0
)

func (s *Service) writeRichMessages(ctx context.Context, chats map[string]string, username string) error {
	s.Clock.Set(time.Now().Add(-10 * time.Minute))

	if err := s.sendPhotos(ctx, chats["weekend"]); err != nil {
		return err
	}

	s.Clock.Set(time.Now().Add(-8 * time.Minute))

	if err := s.sendMusic(ctx, chats["weekend"]); err != nil {
		return err
	}

	s.Clock.Set(time.Now().Add(-6 * time.Minute))

	_, err := s.Messaging.SendContent(ctx, principalOf(subjectOf(9)), chats["news"], "seed-forward", message.Outgoing{
		Text:        "Группы и каналы уже работают, загляните в поиск",
		ForwardName: "Новости Airlance",
	})
	if err != nil {
		return fmt.Errorf("forward: %w", err)
	}

	if username == "" {
		return nil
	}

	s.Clock.Set(time.Now().Add(-4 * time.Minute))

	text := fmt.Sprintf("@%s посмотри, пожалуйста, новую сборку", username)
	if _, err := s.Messaging.Send(ctx, principalOf(subjectOf(6)), chats["team"], "seed-mention", text); err != nil {
		return fmt.Errorf("mention: %w", err)
	}

	return nil
}

func (s *Service) sendPhotos(ctx context.Context, chatID string) error {
	owner := 0
	attachments := make([]message.Attachment, 0, 3)

	for index := range 3 {
		data, err := avatar(people[index+1].color)
		if err != nil {
			return err
		}

		id, err := s.storeFile(ctx, owner, fmt.Sprintf("seed-photo-%d", index), data)
		if err != nil {
			return err
		}

		attachments = append(attachments, message.Attachment{
			Kind: message.AttachmentPhoto, MediaID: id, ThumbMediaID: id,
			Size: int64(len(data)), Width: avatarSide, Height: avatarSide,
		})
	}

	_, err := s.Messaging.SendContent(ctx, principalOf(subjectOf(owner)), chatID, "seed-photos", message.Outgoing{
		Text:        "Фото с прошлого раза",
		Attachments: attachments,
	})
	if err != nil {
		return fmt.Errorf("photos: %w", err)
	}

	return nil
}

func (s *Service) sendMusic(ctx context.Context, chatID string) error {
	owner := 2
	data := tone()

	id, err := s.storeFile(ctx, owner, "seed-music", data)
	if err != nil {
		return err
	}

	_, err = s.Messaging.SendContent(ctx, principalOf(subjectOf(owner)), chatID, "seed-music", message.Outgoing{
		Attachments: []message.Attachment{{
			Kind: message.AttachmentMusic, MediaID: id, Name: "Ля первой октавы.wav", Size: int64(len(data)),
			DurationMs: soundSeconds * 1000, Title: "Ля первой октавы", Performer: "Камертон",
		}},
	})
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}

	return nil
}

func (s *Service) storeFile(ctx context.Context, owner int, name string, data []byte) (string, error) {
	id := fmt.Sprintf("%x", []byte(name))

	if _, err := s.Media.Get(ctx, id); err == nil {
		return id, nil
	} else if !errors.Is(err, media.ErrNotFound) {
		return "", fmt.Errorf("read file: %w", err)
	}

	object, err := media.New(id, subjectOf(owner), int64(len(data)), time.Now())
	if err != nil {
		return "", fmt.Errorf("check file: %w", err)
	}

	if err := s.Files.Put(ctx, id, data); err != nil {
		return "", fmt.Errorf("store file: %w", err)
	}

	if err := s.Media.Create(ctx, object); err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}

	return id, nil
}

func tone() []byte {
	samples := soundRate * soundSeconds

	var out bytes.Buffer

	out.WriteString("RIFF")
	write(&out, uint32(36+samples*2))
	out.WriteString("WAVEfmt ")
	write(&out, uint32(16))
	write(&out, uint16(1))
	write(&out, uint16(1))
	write(&out, uint32(soundRate))
	write(&out, uint32(soundRate*2))
	write(&out, uint16(2))
	write(&out, uint16(16))
	out.WriteString("data")
	write(&out, uint32(samples*2))

	for index := range samples {
		write(&out, int16(soundVolume*math.Sin(2*math.Pi*soundPitch*float64(index)/soundRate)))
	}

	return out.Bytes()
}

func write(out *bytes.Buffer, value any) {
	if err := binary.Write(out, binary.LittleEndian, value); err != nil {
		panic(err)
	}
}
