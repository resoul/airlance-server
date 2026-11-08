package message

import (
	"errors"
	"strings"
	"time"
)

const (
	MaxPollQuestion    = 255
	MaxPollOptionText  = 100
	MaxPollExplanation = 200
	MinPollOptions     = 2
	MaxPollOptions     = 10
	MaxPollDuration    = 366 * 24 * time.Hour
	MaxPollVoters      = 3
)

var (
	ErrInvalidPoll  = errors.New("invalid poll")
	ErrInvalidVote  = errors.New("invalid vote")
	ErrPollClosed   = errors.New("poll is closed")
	ErrAlreadyVoted = errors.New("already voted")
)

type PollOption struct {
	Text   string
	Votes  int32
	Voters []Commenter
}

type Poll struct {
	Question    string
	Options     []PollOption
	Anonymous   bool
	Multiple    bool
	Quiz        bool
	Correct     int32
	Explanation string
	CloseAt     time.Time
	ClosedAt    time.Time
	IsClosed    bool
	TotalVoters int32
	Mine        []int32
}

func NewPoll(draft Poll, now time.Time) (Poll, error) {
	question := strings.TrimSpace(draft.Question)
	if question == "" || !isShort(question, MaxPollQuestion) {
		return Poll{}, ErrInvalidPoll
	}

	if len(draft.Options) < MinPollOptions || len(draft.Options) > MaxPollOptions {
		return Poll{}, ErrInvalidPoll
	}

	options := make([]PollOption, len(draft.Options))
	seen := map[string]struct{}{}

	for index, next := range draft.Options {
		text := strings.TrimSpace(next.Text)
		if _, repeated := seen[text]; text == "" || !isShort(text, MaxPollOptionText) || repeated {
			return Poll{}, ErrInvalidPoll
		}

		seen[text] = struct{}{}
		options[index] = PollOption{Text: text}
	}

	poll := Poll{
		Question: question, Options: options, Anonymous: draft.Anonymous, Multiple: draft.Multiple,
		Quiz: draft.Quiz, Correct: -1, Explanation: strings.TrimSpace(draft.Explanation), CloseAt: draft.CloseAt,
	}

	return poll, poll.check(draft.Correct, now)
}

func (p *Poll) check(correct int32, now time.Time) error {
	if !isShort(p.Explanation, MaxPollExplanation) {
		return ErrInvalidPoll
	}

	if !p.CloseAt.IsZero() && (!p.CloseAt.After(now) || p.CloseAt.After(now.Add(MaxPollDuration))) {
		return ErrInvalidPoll
	}

	if !p.Quiz {
		if p.Explanation != "" {
			return ErrInvalidPoll
		}

		return nil
	}

	if p.Multiple || correct < 0 || int(correct) >= len(p.Options) {
		return ErrInvalidPoll
	}

	p.Correct = correct

	return nil
}

func (p Poll) Finished(now time.Time) bool {
	return p.IsClosed || !p.ClosedAt.IsZero() || (!p.CloseAt.IsZero() && !p.CloseAt.After(now))
}

func (p Poll) CheckVote(options []int32) error {
	if len(options) > len(p.Options) || (len(options) == 0 && p.Quiz) {
		return ErrInvalidVote
	}

	if len(options) > 1 && !p.Multiple {
		return ErrInvalidVote
	}

	seen := map[int32]struct{}{}

	for _, next := range options {
		if _, repeated := seen[next]; next < 0 || int(next) >= len(p.Options) || repeated {
			return ErrInvalidVote
		}

		seen[next] = struct{}{}
	}

	return nil
}
