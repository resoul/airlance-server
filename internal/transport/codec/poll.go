package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindPollVote  = Kind(wire.MessageKindPollVote)
	KindPollClose = Kind(wire.MessageKindPollClose)

	maxPollOptions = 16
)

type PollOption struct {
	Text   string
	Votes  int32
	Voters []PostCommenter
}

type Poll struct {
	Question      string
	Options       []PollOption
	Anonymous     bool
	Multiple      bool
	IsQuiz        bool
	CorrectOption int32
	Explanation   string
	CloseAtMs     int64
	IsClosed      bool
	TotalVoters   int32
	MyChoices     []int32
}

type PollVote struct {
	ChatID    string
	MessageID int64
	Options   []int32
}

func buildPoll(builder *flatbuffers.Builder, poll *Poll) flatbuffers.UOffsetT {
	if poll == nil {
		return 0
	}

	question := builder.CreateString(poll.Question)
	explanation := builder.CreateString(poll.Explanation)
	options := buildPollOptions(builder, poll.Options)

	wire.PollDataStartMyChoicesVector(builder, len(poll.MyChoices))

	for index := len(poll.MyChoices) - 1; index >= 0; index-- {
		builder.PrependInt32(poll.MyChoices[index])
	}

	mine := builder.EndVector(len(poll.MyChoices))
	wire.PollDataStart(builder)
	wire.PollDataAddQuestion(builder, question)
	wire.PollDataAddOptions(builder, options)
	wire.PollDataAddAnonymous(builder, poll.Anonymous)
	wire.PollDataAddMultiple(builder, poll.Multiple)
	wire.PollDataAddIsQuiz(builder, poll.IsQuiz)
	wire.PollDataAddCorrectOption(builder, poll.CorrectOption)
	wire.PollDataAddExplanation(builder, explanation)
	wire.PollDataAddCloseAtUnixMs(builder, poll.CloseAtMs)
	wire.PollDataAddIsClosed(builder, poll.IsClosed)
	wire.PollDataAddTotalVoters(builder, poll.TotalVoters)
	wire.PollDataAddMyChoices(builder, mine)

	return wire.PollDataEnd(builder)
}

func buildPollOptions(builder *flatbuffers.Builder, options []PollOption) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(options))

	for index, next := range options {
		text := builder.CreateString(next.Text)
		voters := buildVoters(builder, next.Voters)
		wire.PollOptionStart(builder)
		wire.PollOptionAddText(builder, text)
		wire.PollOptionAddVotes(builder, next.Votes)
		wire.PollOptionAddVoters(builder, voters)
		offsets[index] = wire.PollOptionEnd(builder)
	}

	wire.PollDataStartOptionsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func buildVoters(builder *flatbuffers.Builder, voters []PostCommenter) flatbuffers.UOffsetT {
	people := make([]flatbuffers.UOffsetT, len(voters))

	for index, next := range voters {
		subject := builder.CreateString(next.Subject)
		first := builder.CreateString(next.FirstName)
		last := builder.CreateString(next.LastName)
		photo := builder.CreateString(next.PhotoMediaID)
		video := builder.CreateString(next.VideoMediaID)
		wire.PostCommenterStart(builder)
		wire.PostCommenterAddSubject(builder, subject)
		wire.PostCommenterAddFirstName(builder, first)
		wire.PostCommenterAddLastName(builder, last)
		wire.PostCommenterAddPhotoMediaId(builder, photo)
		wire.PostCommenterAddVideoMediaId(builder, video)
		people[index] = wire.PostCommenterEnd(builder)
	}

	wire.PollOptionStartVotersVector(builder, len(people))

	for index := len(people) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(people[index])
	}

	return builder.EndVector(len(people))
}

func readPoll(poll *wire.PollData) *Poll {
	if poll == nil {
		return nil
	}

	if poll.OptionsLength() > maxPollOptions || poll.MyChoicesLength() > maxPollOptions {
		panic("too many poll options")
	}

	read := &Poll{
		Question:      string(poll.Question()),
		Anonymous:     poll.Anonymous(),
		Multiple:      poll.Multiple(),
		IsQuiz:        poll.IsQuiz(),
		CorrectOption: poll.CorrectOption(),
		Explanation:   string(poll.Explanation()),
		CloseAtMs:     poll.CloseAtUnixMs(),
		IsClosed:      poll.IsClosed(),
		TotalVoters:   poll.TotalVoters(),
	}

	var option wire.PollOption

	for index := range poll.OptionsLength() {
		if !poll.Options(&option, index) {
			panic("poll option")
		}

		next := PollOption{Text: string(option.Text()), Votes: option.Votes()}
		next.Voters = readVoters(&option)
		read.Options = append(read.Options, next)
	}

	for index := range poll.MyChoicesLength() {
		read.MyChoices = append(read.MyChoices, poll.MyChoices(index))
	}

	return read
}

func readVoters(option *wire.PollOption) []PostCommenter {
	if option.VotersLength() > maxStatItems {
		panic("too many voters")
	}

	var (
		voters []PostCommenter
		person wire.PostCommenter
	)

	for index := range option.VotersLength() {
		if !option.Voters(&person, index) {
			panic("voter")
		}

		voters = append(voters, PostCommenter{
			Subject:      string(person.Subject()),
			FirstName:    string(person.FirstName()),
			LastName:     string(person.LastName()),
			PhotoMediaID: string(person.PhotoMediaId()),
			VideoMediaID: string(person.VideoMediaId()),
		})
	}

	return voters
}

func DecodePollVote(body []byte) (PollVote, error) {
	var vote PollVote

	err := safely(func() {
		table := wire.GetRootAsPollVote(body, 0)
		if table.OptionsLength() > maxPollOptions {
			panic("too many options")
		}

		vote = PollVote{ChatID: string(table.ChatId()), MessageID: table.MessageId()}

		for index := range table.OptionsLength() {
			vote.Options = append(vote.Options, table.Options(index))
		}
	})

	return vote, err
}

func EncodePollVote(vote PollVote) []byte {
	builder := flatbuffers.NewBuilder(96)
	chat := builder.CreateString(vote.ChatID)
	wire.PollVoteStartOptionsVector(builder, len(vote.Options))

	for index := len(vote.Options) - 1; index >= 0; index-- {
		builder.PrependInt32(vote.Options[index])
	}

	options := builder.EndVector(len(vote.Options))
	wire.PollVoteStart(builder)
	wire.PollVoteAddChatId(builder, chat)
	wire.PollVoteAddMessageId(builder, vote.MessageID)
	wire.PollVoteAddOptions(builder, options)
	builder.Finish(wire.PollVoteEnd(builder))

	return builder.FinishedBytes()
}

func DecodePollClose(body []byte) (string, int64, error) {
	var (
		chatID string
		id     int64
	)

	err := safely(func() {
		table := wire.GetRootAsPollClose(body, 0)
		chatID, id = string(table.ChatId()), table.MessageId()
	})

	return chatID, id, err
}

func EncodePollClose(chatID string, id int64) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(chatID)
	wire.PollCloseStart(builder)
	wire.PollCloseAddChatId(builder, chat)
	wire.PollCloseAddMessageId(builder, id)
	builder.Finish(wire.PollCloseEnd(builder))

	return builder.FinishedBytes()
}
