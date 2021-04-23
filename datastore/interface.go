//go:generate protoc --go_out=module=github.com/ferocious-space/evesso/datastore:. proto/*.proto
//go:generate protoc-go-inject-tag -input=./AccountData.pb.go

package datastore

import (
	"reflect"

	"github.com/pkg/errors"
)

var ErrInvalidToken = errors.New("token cannot be converted to AccountData")
var ErrNotFound = errors.New("token not found.")
var ErrAlreadyExists = errors.New("account already exists.")
var ErrOwner = errors.New("Owner dont match.")
var ErrCharacterID = errors.New("characterID dont match.")
var ErrCharacterName = errors.New("characterName dont match.")
var ErrScope = errors.New("scopes dont match.")

// type AccountData struct {
// 	CharacterName string   `bson:"character_name" json:"character_name"`
// 	CharacterId   int32    `bson:"character_id" json:"character_id"`
// 	Owner         string   `bson:"owner" json:"owner"`
// 	RefreshToken  string   `bson:"refresh_token" json:"refresh_token"`
// 	Scopes        []string `bson:"scopes" json:"scopes"`
// }

func (x AccountData) IsZero() bool {
	return reflect.DeepEqual(x, AccountData{})
}

func (x *AccountData) Valid() bool {
	if len(x.CharacterName) == 0 {
		return false
	}
	if x.CharacterId <= 0 {
		return false
	}
	if len(x.Owner) == 0 {
		return false
	}
	if len(x.RefreshToken) == 0 {
		return false
	}
	if len(x.Scopes) == 0 {
		return false
	}
	return true
}

type AccountStore interface {
	Create(data *AccountData) error
	SearchName(CharacterName string, Scopes []string) (data *AccountData, err error)
	SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error)
	Update(data *AccountData) error
	Delete(data *AccountData) error
}

func MatchScopes(x, y []string) bool {
	xLen := len(x)
	yLen := len(y)

	if xLen != yLen {
		return false
	}

	if xLen > 20 {
		return elementsMatchByMap(x, y)
	} else {
		return elementsMatchByLoop(x, y)
	}

}

func elementsMatchByLoop(x, y []string) bool {
	xLen := len(x)
	yLen := len(y)

	visited := make([]bool, yLen)

	for i := 0; i < xLen; i++ {
		found := false
		element := x[i]
		for j := 0; j < yLen; j++ {
			if visited[j] {
				continue
			}
			if element == y[j] {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func elementsMatchByMap(x, y []string) bool {
	// create a map of string -> int
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y] -= 1
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	if len(diff) == 0 {
		return true
	}
	return false
}
