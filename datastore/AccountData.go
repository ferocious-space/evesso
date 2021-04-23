package datastore

import (
	"fmt"
	"sort"

	"github.com/lestrrat-go/jwx/jwt"
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
