package datastore

import (
	"fmt"
	"sort"

	"github.com/ferocious-space/bolthold"
	jsoniter "github.com/json-iterator/go"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

func NewAccountData(token *oauth2.Token) (*AccountData, error) {
	data := new(AccountData)
	err := data.FromToken(token)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (x *AccountData) FromToken(token *oauth2.Token) error {
	jToken, err := jwt.Parse([]byte(token.AccessToken))
	if err != nil {
		return err
	}

	var characterName, owner string
	var characterID int32
	var scope []string

	scp, ok := jToken.Get("scp")
	if !ok {
		return ErrScope
	}

	switch scp.(type) {
	case string:
		scope = append([]string{}, scp.(string))
	default:
		for k := range scp.([]interface{}) {
			scope = append(scope, scp.([]interface{})[k].(string))
		}
	}

	if CharacterName, ok := jToken.Get("name"); !ok {
		return ErrCharacterName
	} else {
		characterName = CharacterName.(string)
	}
	if Owner, ok := jToken.Get("owner"); !ok {
		return ErrCharacterName
	} else {
		owner = Owner.(string)
	}

	subj := jToken.Subject()
	if n, err := fmt.Sscanf(subj, "CHARACTER:EVE:%d", &characterID); err != nil || n != 1 {
		return ErrCharacterID
	}
	sort.Strings(scope)
	x.Reset()
	x.RefreshToken = token.RefreshToken
	x.CharacterName = characterName
	x.CharacterId = characterID
	x.Owner = owner
	x.Scopes = scope
	return nil
}

func (x *AccountData) Type() string {
	return "AccountData"
}

func (x *AccountData) Indexes() map[string]bolthold.Index {
	return map[string]bolthold.Index{
		"CharacterName": func(name string, value interface{}) ([]byte, error) {
			data, ok := value.(*AccountData)
			if !ok {
				return nil, errors.New("invalid data passed to index")
			}
			return jsoniter.Marshal(data.CharacterName)
		},
		"CharacterId": func(name string, value interface{}) ([]byte, error) {
			data, ok := value.(*AccountData)
			if !ok {
				return nil, errors.New("invalid data passed to index")
			}
			return jsoniter.Marshal(data.CharacterId)
		},
	}
}

func (x *AccountData) SliceIndexes() map[string]bolthold.SliceIndex {
	return map[string]bolthold.SliceIndex{
		"Scopes": func(name string, value interface{}) ([][]byte, error) {
			data, ok := value.(*AccountData)
			if !ok {
				return nil, errors.New("invalid data passed to index")
			}
			var out [][]byte
			for _, s := range data.Scopes {
				bin, err := jsoniter.Marshal(s)
				if err != nil {
					return nil, err
				}
				out = append(out, bin)
			}
			return out, nil
		},
	}
}
