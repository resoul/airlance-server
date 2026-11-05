package folder

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MaxFolders     = 50
	MaxNameLength  = 24
	MaxEmojiLength = 16
	MaxIDLength    = 64
	MaxPeers       = 500
	MaxColor       = 6
	NoColor        = -1
	MaxCategories  = 31
	MaxExclusions  = 3
)

var ErrInvalid = errors.New("invalid folder list")

type Folder struct {
	ID         string
	Name       string
	Emoji      string
	Color      int32
	Categories int32
	Exclusions int32
	Included   []string
	Excluded   []string
}

type List struct {
	Folders          []Folder
	AllChatsPosition int32
	ShowsTags        bool
	Revision         int64
}

func Empty() List {
	return List{ShowsTags: true}
}

func (l List) Checked() (List, error) {
	if len(l.Folders) > MaxFolders || l.AllChatsPosition < 0 || int(l.AllChatsPosition) > len(l.Folders) {
		return List{}, ErrInvalid
	}

	seen := make(map[string]struct{}, len(l.Folders))

	for index, next := range l.Folders {
		checked, err := next.checked()
		if err != nil {
			return List{}, err
		}

		if _, repeated := seen[checked.ID]; repeated {
			return List{}, ErrInvalid
		}

		seen[checked.ID] = struct{}{}
		l.Folders[index] = checked
	}

	return l, nil
}

func (f Folder) checked() (Folder, error) {
	f.Name = strings.TrimSpace(f.Name)

	switch {
	case !isID(f.ID):
		return Folder{}, ErrInvalid
	case f.Name == "" || !utf8.ValidString(f.Name) || utf8.RuneCountInString(f.Name) > MaxNameLength:
		return Folder{}, ErrInvalid
	case !utf8.ValidString(f.Emoji) || utf8.RuneCountInString(f.Emoji) > MaxEmojiLength:
		return Folder{}, ErrInvalid
	case f.Color < NoColor || f.Color > MaxColor:
		return Folder{}, ErrInvalid
	case f.Categories < 0 || f.Categories > MaxCategories || f.Exclusions < 0 || f.Exclusions > MaxExclusions:
		return Folder{}, ErrInvalid
	case !arePeers(f.Included) || !arePeers(f.Excluded):
		return Folder{}, ErrInvalid
	}

	return f, nil
}

func arePeers(peers []string) bool {
	if len(peers) > MaxPeers {
		return false
	}

	for _, peer := range peers {
		if !isID(peer) {
			return false
		}
	}

	return true
}

func isID(id string) bool {
	return id != "" && len(id) <= MaxIDLength && utf8.ValidString(id)
}
