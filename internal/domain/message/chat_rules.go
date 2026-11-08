package message

import "slices"

const (
	ReactionsAll  uint8 = 0
	ReactionsSome uint8 = 1
	ReactionsNone uint8 = 2

	MaxRuleReactions    = 100
	MaxReactionsPerPost = 11
)

type ChatRules struct {
	NoCopy        bool
	Signs         bool
	ReactionsMode uint8
	Reactions     []string
	PerPost       uint8
}

func (r ChatRules) Checked() (ChatRules, error) {
	if r.ReactionsMode > ReactionsNone || r.PerPost > MaxReactionsPerPost {
		return ChatRules{}, ErrInvalidReaction
	}

	if r.ReactionsMode != ReactionsSome {
		r.Reactions = nil

		return r, nil
	}

	unique := make([]string, 0, len(r.Reactions))

	for _, emoji := range r.Reactions {
		if emoji == "" {
			return ChatRules{}, ErrInvalidReaction
		}

		if err := CheckReaction(emoji); err != nil {
			return ChatRules{}, err
		}

		if !slices.Contains(unique, emoji) {
			unique = append(unique, emoji)
		}
	}

	if len(unique) == 0 || len(unique) > MaxRuleReactions {
		return ChatRules{}, ErrInvalidReaction
	}

	r.Reactions = unique

	return r, nil
}

func (r ChatRules) AllowsReaction(emoji string) bool {
	switch r.ReactionsMode {
	case ReactionsNone:
		return false
	case ReactionsSome:
		return slices.Contains(r.Reactions, emoji)
	default:
		return true
	}
}
