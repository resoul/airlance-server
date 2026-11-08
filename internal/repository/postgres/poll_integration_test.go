//go:build integration

package postgres_test

import (
	"errors"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/message"
)

func (s *stage) poll(t *testing.T, who string, chatID, clientID string, draft domain.Poll) domain.Message {
	t.Helper()

	sent, err := s.messages.SendContent(s.ctx, person(who), chatID, clientID, domain.Outgoing{Poll: &draft})
	if err != nil {
		t.Fatal(err)
	}

	return sent
}

func TestPollsCountVotesOncePerPerson(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "olena")
	chat := s.group(t, "anna", "petro", "olena")

	sent := s.poll(t, "anna", chat.ID, "poll", domain.Poll{
		Question: "Куди?", Multiple: true, Options: []domain.PollOption{{Text: "Ліс"}, {Text: "Море"}, {Text: "Гори"}},
	})

	if sent.Poll == nil || sent.Text != "Куди?" {
		t.Fatalf("got %+v", sent)
	}

	if _, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{0, 1}); err != nil {
		t.Fatal(err)
	}

	changed, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{1})
	if err != nil || changed.Poll.TotalVoters != 1 || changed.Poll.Options[0].Votes != 0 || changed.Poll.Options[1].Votes != 1 {
		t.Fatalf("a changed vote was counted twice: %+v, %v", changed.Poll, err)
	}

	if _, err := s.messages.Vote(s.ctx, person("olena"), chat.ID, sent.ID, []int32{1, 2}); err != nil {
		t.Fatal(err)
	}

	seen := s.history(t, "anna", chat.ID)[0].Poll
	if seen.TotalVoters != 2 || seen.Options[1].Votes != 2 || len(seen.Options[1].Voters) != 2 || len(seen.Mine) != 0 {
		t.Fatalf("got %+v", seen)
	}

	mine := s.history(t, "olena", chat.ID)[0].Poll
	if len(mine.Mine) != 2 || mine.Mine[0] != 1 || mine.Mine[1] != 2 {
		t.Fatalf("got %+v", mine)
	}

	retracted, err := s.messages.Vote(s.ctx, person("olena"), chat.ID, sent.ID, nil)
	if err != nil || retracted.Poll.TotalVoters != 1 || len(retracted.Poll.Mine) != 0 {
		t.Fatalf("got %+v, %v", retracted.Poll, err)
	}

	if _, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{9}); !errors.Is(err, domain.ErrInvalidVote) {
		t.Fatalf("a missing option: %v", err)
	}

	if _, err := s.messages.Edit(s.ctx, person("anna"), chat.ID, sent.ID, "иначе", nil); !errors.Is(err, domain.ErrNothingToEdit) {
		t.Fatalf("a poll was edited: %v", err)
	}
}

func TestQuizAnswersAreFinalAndOnlyTheAuthorCloses(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	sent := s.poll(t, "anna", chat.ID, "quiz", domain.Poll{
		Question: "2+2?", Quiz: true, Correct: 1, Explanation: "арифметика", Anonymous: true,
		Options: []domain.PollOption{{Text: "3"}, {Text: "4"}},
	})

	if sent.Poll.Correct != 1 || !sent.Poll.Quiz {
		t.Fatalf("got %+v", sent.Poll)
	}

	hidden := s.history(t, "petro", chat.ID)[0].Poll
	if hidden.Correct != -1 || hidden.Explanation != "" {
		t.Fatalf("the answer leaked before voting: %+v", hidden)
	}

	if _, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, nil); !errors.Is(err, domain.ErrInvalidVote) {
		t.Fatalf("a quiz was left empty: %v", err)
	}

	voted, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{0})
	if err != nil {
		t.Fatal(err)
	}

	if voted.Poll.Correct != 1 || voted.Poll.Explanation != "арифметика" {
		t.Fatalf("the answer stayed hidden after voting: %+v", voted.Poll)
	}

	if _, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{1}); !errors.Is(err, domain.ErrAlreadyVoted) {
		t.Fatalf("a quiz answer changed: %v", err)
	}

	if got := s.history(t, "anna", chat.ID)[0].Poll; len(got.Options[0].Voters) != 0 {
		t.Fatalf("an anonymous poll names voters: %+v", got)
	}

	if _, err := s.messages.ClosePoll(s.ctx, person("petro"), chat.ID, sent.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member closed the poll: %v", err)
	}

	closed, err := s.messages.ClosePoll(s.ctx, person("anna"), chat.ID, sent.ID)
	if err != nil || !closed.Poll.IsClosed {
		t.Fatalf("got %+v, %v", closed.Poll, err)
	}

	if _, err := s.messages.Vote(s.ctx, person("petro"), chat.ID, sent.ID, []int32{1}); !errors.Is(err, domain.ErrPollClosed) {
		t.Fatalf("voted after the end: %v", err)
	}

	later := s.poll(t, "anna", chat.ID, "late", domain.Poll{
		Question: "Скоро?", CloseAt: time.Now().Add(time.Hour), Options: []domain.PollOption{{Text: "так"}, {Text: "ні"}},
	})
	if later.Poll.CloseAt.IsZero() {
		t.Fatalf("the deadline was lost: %+v", later.Poll)
	}
}
