package privacy

import "errors"

type Level int32

const (
	Everyone Level = 0
	Contacts Level = 1
	Nobody   Level = 2
)

var (
	ErrInvalidSettings = errors.New("invalid privacy settings")
	ErrInvalidPeer     = errors.New("invalid peer to block")
)

type Settings struct {
	Phone    Level
	Photos   Level
	Bio      Level
	Messages Level
	Search   Level
}

type Relation struct {
	IsContact       bool
	SharesChat      bool
	BlockedByTarget bool
	BlocksTarget    bool
}

func Defaults() Settings {
	return Settings{Phone: Nobody, Photos: Everyone, Bio: Everyone, Messages: Everyone, Search: Everyone}
}

func (s Settings) Validate() error {
	for _, level := range []Level{s.Phone, s.Photos, s.Bio} {
		if level < Everyone || level > Nobody {
			return ErrInvalidSettings
		}
	}

	if s.Messages != Everyone && s.Messages != Contacts {
		return ErrInvalidSettings
	}

	if s.Search != Everyone && s.Search != Nobody {
		return ErrInvalidSettings
	}

	return nil
}

func (l Level) Allows(relation Relation) bool {
	switch l {
	case Everyone:
		return true
	case Contacts:
		return relation.IsContact
	default:
		return false
	}
}
