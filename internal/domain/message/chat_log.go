package message

import "time"

type LogKind uint8

const (
	LogRenamed LogKind = iota + 1
	LogAboutChanged
	LogMadePublic
	LogMadePrivate
	LogLinkChanged
	LogCopyingRestricted
	LogCopyingAllowed
	LogReactionsChanged
	LogSignaturesOn
	LogSignaturesOff
	LogAdminAdded
	LogAdminRemoved
	LogBanned
	LogUnbanned
)

const MaxLogPage = 100

type LogEntry struct {
	ID     int64
	Actor  Peer
	Kind   LogKind
	Detail string
	At     time.Time
}

func (r ChatRules) Changes(next ChatRules) []LogKind {
	var found []LogKind

	if r.NoCopy != next.NoCopy {
		found = append(found, copyKind(next.NoCopy))
	}

	if r.Signs != next.Signs {
		found = append(found, signsKind(next.Signs))
	}

	if r.reactionsDiffer(next) {
		found = append(found, LogReactionsChanged)
	}

	return found
}

func (r ChatRules) reactionsDiffer(next ChatRules) bool {
	if r.ReactionsMode != next.ReactionsMode || r.PerPost != next.PerPost || len(r.Reactions) != len(next.Reactions) {
		return true
	}

	for index, emoji := range r.Reactions {
		if next.Reactions[index] != emoji {
			return true
		}
	}

	return false
}

func copyKind(restricted bool) LogKind {
	if restricted {
		return LogCopyingRestricted
	}

	return LogCopyingAllowed
}

func signsKind(isOn bool) LogKind {
	if isOn {
		return LogSignaturesOn
	}

	return LogSignaturesOff
}

func InfoChanges(before, next ChatInfo) []LogEntry {
	var found []LogEntry

	if before.Title != next.Title {
		found = append(found, LogEntry{Kind: LogRenamed, Detail: next.Title})
	}

	if before.About != next.About {
		found = append(found, LogEntry{Kind: LogAboutChanged})
	}

	if before.Link != next.Link {
		found = append(found, linkEntry(before.Link, next.Link))
	}

	return found
}

func linkEntry(before, link string) LogEntry {
	switch {
	case link == "":
		return LogEntry{Kind: LogMadePrivate}
	case before == "":
		return LogEntry{Kind: LogMadePublic, Detail: link}
	default:
		return LogEntry{Kind: LogLinkChanged, Detail: link}
	}
}

const MaxReadersPage = 100
