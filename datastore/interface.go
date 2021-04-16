package datastore

import (
	"reflect"

	"github.com/pkg/errors"
)

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
