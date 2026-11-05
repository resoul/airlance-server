package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

type attachmentRow struct {
	Kind         int32  `json:"kind"`
	MediaID      string `json:"media_id"`
	ThumbMediaID string `json:"thumb_media_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Width        int32  `json:"width,omitempty"`
	Height       int32  `json:"height,omitempty"`
	DurationMs   int32  `json:"duration_ms,omitempty"`
	Waveform     []byte `json:"waveform,omitempty"`
	Title        string `json:"title,omitempty"`
	Performer    string `json:"performer,omitempty"`
}

func encodeAttachments(attachments []domain.Attachment) ([]byte, error) {
	rows := make([]attachmentRow, len(attachments))
	for index, next := range attachments {
		rows[index] = attachmentRow{
			Kind: int32(next.Kind), MediaID: next.MediaID, ThumbMediaID: next.ThumbMediaID, Name: next.Name,
			Size: next.Size, Width: next.Width, Height: next.Height, DurationMs: next.DurationMs,
			Waveform: next.Waveform, Title: next.Title, Performer: next.Performer,
		}
	}

	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encode attachments: %w", err)
	}

	return encoded, nil
}

func decodeAttachments(raw []byte) ([]domain.Attachment, error) {
	var rows []attachmentRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode attachments: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	attachments := make([]domain.Attachment, len(rows))
	for index, next := range rows {
		attachments[index] = domain.Attachment{
			Kind: domain.AttachmentKind(next.Kind), MediaID: next.MediaID, ThumbMediaID: next.ThumbMediaID, Name: next.Name,
			Size: next.Size, Width: next.Width, Height: next.Height, DurationMs: next.DurationMs,
			Waveform: next.Waveform, Title: next.Title, Performer: next.Performer,
		}
	}

	return attachments, nil
}

func checkOwnsMedia(ctx context.Context, tx pgx.Tx, sender string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	var owned int

	err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT id) FROM media_objects WHERE id = ANY($1) AND owner = $2`, ids, sender,
	).Scan(&owned)
	if err != nil {
		return fmt.Errorf("check media: %w", err)
	}

	distinct := map[string]struct{}{}
	for _, id := range ids {
		distinct[id] = struct{}{}
	}

	if owned != len(distinct) {
		return domain.ErrInvalidAttachment
	}

	return nil
}

func linkMedia(ctx context.Context, tx pgx.Tx, chatID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO message_media (media_id, chat_id)
		SELECT unnest($1::text[]), $2
		ON CONFLICT DO NOTHING`, ids, chatID,
	)
	if err != nil {
		return fmt.Errorf("link media: %w", err)
	}

	return nil
}
