package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/agent"
)

const agentColumns = `a.id, a.owner, a.client_id, a.name, a.chat_id, COALESCE(c.title, ''), a.trigger_kind, a.keyword,
	a.hour, a.minute, a.weekdays, a.timezone, a.text, a.enabled, COALESCE(a.next_run_at, 'epoch'::timestamptz)`

type Agents struct {
	pool *pgxpool.Pool
}

func NewAgents(pool *pgxpool.Pool) *Agents {
	return &Agents{pool: pool}
}

func collectAgents(rows pgx.Rows) ([]domain.Agent, error) {
	agents, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Agent, error) {
		var next domain.Agent

		err := row.Scan(
			&next.ID, &next.Owner, &next.ClientID, &next.Name, &next.ChatID, &next.ChatTitle, &next.Trigger, &next.Keyword,
			&next.Hour, &next.Minute, &next.Weekdays, &next.Timezone, &next.Text, &next.Enabled, &next.NextRunAt,
		)

		return next, err
	})
	if err != nil {
		return nil, fmt.Errorf("read agents: %w", err)
	}

	return agents, nil
}

func (r *Agents) Save(ctx context.Context, next domain.Agent, webhookHash string, now time.Time) (domain.Agent, error) {
	var nextRun *time.Time
	if next.Trigger == domain.TriggerSchedule && next.Enabled {
		nextRun = &next.NextRunAt
	}

	tag, err := r.pool.Exec(ctx, `
		INSERT INTO agents (
			id, owner, client_id, name, chat_id, trigger_kind, keyword, hour, minute, weekdays, timezone, text,
			webhook_hash, enabled, next_run_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''), $14, $15, $16, $16)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name, chat_id = excluded.chat_id, trigger_kind = excluded.trigger_kind,
			keyword = excluded.keyword, hour = excluded.hour, minute = excluded.minute, weekdays = excluded.weekdays,
			timezone = excluded.timezone, text = excluded.text, enabled = excluded.enabled,
			webhook_hash = COALESCE(excluded.webhook_hash, agents.webhook_hash),
			next_run_at = excluded.next_run_at, leased_until = NULL, updated_at = excluded.updated_at
		WHERE agents.owner = excluded.owner`,
		next.ID, next.Owner, next.ClientID, next.Name, next.ChatID, next.Trigger, next.Keyword, next.Hour, next.Minute,
		next.Weekdays, next.Timezone, next.Text, webhookHash, next.Enabled, nextRun, now,
	)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("save agent: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.Agent{}, domain.ErrNotFound
	}

	return next, nil
}

func (r *Agents) Count(ctx context.Context, owner string) (int, error) {
	var count int

	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM agents WHERE owner = $1`, owner).Scan(&count); err != nil {
		return 0, fmt.Errorf("count agents: %w", err)
	}

	return count, nil
}

func (r *Agents) List(ctx context.Context, owner string) ([]domain.Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentColumns+` FROM agents a LEFT JOIN chats c ON c.id = a.chat_id
		WHERE a.owner = $1 ORDER BY a.created_at, a.id`, owner,
	)
	if err != nil {
		return nil, fmt.Errorf("select agents: %w", err)
	}

	return collectAgents(rows)
}

func (r *Agents) Delete(ctx context.Context, id, owner string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1 AND owner = $2`, id, owner)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func (r *Agents) Runs(ctx context.Context, id, owner string, limit int) ([]domain.Run, error) {
	var mine bool

	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agents WHERE id = $1 AND owner = $2)`, id, owner).Scan(&mine)
	if err != nil {
		return nil, fmt.Errorf("check agent: %w", err)
	}

	if !mine {
		return nil, domain.ErrNotFound
	}

	rows, err := r.pool.Query(ctx, `
		SELECT status, detail, ran_at FROM agent_runs WHERE agent_id = $1 ORDER BY id DESC LIMIT $2`, id, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select runs: %w", err)
	}

	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Run, error) {
		var next domain.Run

		err := row.Scan(&next.Status, &next.Detail, &next.RanAt)

		return next, err
	})
	if err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}

	return runs, nil
}

func (r *Agents) ByHook(ctx context.Context, hash string) (domain.Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentColumns+` FROM agents a LEFT JOIN chats c ON c.id = a.chat_id
		WHERE a.webhook_hash = $1 AND a.enabled AND a.trigger_kind = $2`, hash, domain.TriggerWebhook,
	)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("select webhook: %w", err)
	}

	agents, err := collectAgents(rows)
	if err != nil {
		return domain.Agent{}, err
	}

	if len(agents) == 0 {
		return domain.Agent{}, domain.ErrNotFound
	}

	return agents[0], nil
}

func (r *Agents) PendingHooks(ctx context.Context, id string) (int, error) {
	var count int

	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM agent_events WHERE agent_id = $1`, id).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}

	return count, nil
}

func (r *Agents) AddHookEvent(ctx context.Context, next domain.Agent, text string, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO agent_events (trigger_kind, chat_id, agent_id, text, created_at) VALUES ($1, $2, $3, $4, $5)`,
		domain.TriggerWebhook, next.ChatID, next.ID, text, now,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func enqueueChatEvent(
	ctx context.Context,
	tx pgx.Tx,
	trigger domain.Trigger,
	chatID, subject, text string,
	now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO agent_events (trigger_kind, chat_id, subject, text, created_at)
		SELECT $1, $2, $3, $4, $5
		WHERE EXISTS (SELECT 1 FROM agents WHERE chat_id = $2 AND trigger_kind = $1 AND enabled)`,
		trigger, chatID, subject, text, now,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func (r *Agents) ClaimEvents(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Event, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE agent_events SET leased_until = $2
		WHERE id IN (
			SELECT id FROM agent_events
			WHERE leased_until IS NULL OR leased_until < $1
			ORDER BY id
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, trigger_kind, chat_id, COALESCE(agent_id, ''), subject, text`, now, leasedUntil, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim events: %w", err)
	}

	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Event, error) {
		var next domain.Event

		err := row.Scan(&next.ID, &next.Trigger, &next.ChatID, &next.AgentID, &next.Subject, &next.Text)

		return next, err
	})
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	return events, nil
}

func (r *Agents) DoneEvent(ctx context.Context, id int64) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM agent_events WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete event: %w", err)
	}

	return nil
}

func (r *Agents) ForEvent(ctx context.Context, event domain.Event) ([]domain.Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentColumns+` FROM agents a LEFT JOIN chats c ON c.id = a.chat_id
		WHERE a.chat_id = $1 AND a.trigger_kind = $2 AND a.enabled`, event.ChatID, event.Trigger,
	)
	if err != nil {
		return nil, fmt.Errorf("select agents: %w", err)
	}

	return collectAgents(rows)
}

func (r *Agents) ClaimDue(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Agent, error) {
	ids, err := r.pool.Query(ctx, `
		UPDATE agents SET leased_until = $2
		WHERE id IN (
			SELECT id FROM agents
			WHERE enabled AND trigger_kind = $4 AND next_run_at <= $1 AND (leased_until IS NULL OR leased_until < $1)
			ORDER BY next_run_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id`, now, leasedUntil, limit, domain.TriggerSchedule,
	)
	if err != nil {
		return nil, fmt.Errorf("claim agents: %w", err)
	}

	claimed, err := pgx.CollectRows(ids, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read claimed agents: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+agentColumns+` FROM agents a LEFT JOIN chats c ON c.id = a.chat_id WHERE a.id = ANY($1)`, claimed,
	)
	if err != nil {
		return nil, fmt.Errorf("select claimed agents: %w", err)
	}

	return collectAgents(rows)
}

func (r *Agents) SetNextRun(ctx context.Context, id string, next time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET next_run_at = $2, leased_until = NULL WHERE id = $1`, id, next)
	if err != nil {
		return fmt.Errorf("update next run: %w", err)
	}

	return nil
}

func (r *Agents) AddRun(ctx context.Context, id string, run domain.Run) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO agent_runs (agent_id, status, detail, ran_at) VALUES ($1, $2, $3, $4)`,
			id, run.Status, run.Detail, run.RanAt,
		)
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}

		_, err = tx.Exec(ctx, `
			DELETE FROM agent_runs WHERE agent_id = $1 AND id NOT IN (
				SELECT id FROM agent_runs WHERE agent_id = $1 ORDER BY id DESC LIMIT $2
			)`, id, domain.KeptRuns,
		)
		if err != nil {
			return fmt.Errorf("prune runs: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("add run: %w", err)
	}

	return nil
}

func (r *Agents) SentSince(ctx context.Context, id string, since time.Time) (int, error) {
	var count int

	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM messages
		WHERE agent_id = $1 AND sent_at >= $2 AND chat_id = (SELECT chat_id FROM agents WHERE id = $1)`, id, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count sent: %w", err)
	}

	return count, nil
}

func (r *Agents) NameOf(ctx context.Context, subject string) (string, error) {
	var first, last string

	err := r.pool.QueryRow(ctx, `SELECT first_name, last_name FROM profiles WHERE subject = $1`, subject).Scan(&first, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("select name: %w", err)
	}

	return strings.TrimSpace(first + " " + last), nil
}
