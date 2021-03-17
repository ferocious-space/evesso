package datastore

import (
	"fmt"

	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"
)

type DataStore struct {
	astore AccountStore
}

func NewDataStore(astore AccountStore) *DataStore {
	return &DataStore{astore: astore}
}

func (ds *DataStore) GetToken(CharacterName string, Scopes ...string) (token *oauth2.Token, err error) {
	data, err := ds.astore.SearchName(CharacterName, Scopes)
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{
		RefreshToken: data.RefreshToken,
	}, nil
}

func (ds *DataStore) SaveToken(token *oauth2.Token) error {
	jToken, err := jwt.Parse([]byte(token.AccessToken))
	if err != nil {
		return err
	}

	var characterName, owner string
	var characterID int64
	var scope []string

	scp, ok := jToken.Get("scp")
	if !ok {
		return ErrScope
	}

	switch scp.(type) {
	case string:
		scope = append([]string{}, scp.(string))
	default:
		scope = scp.([]string)
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

	return ds.astore.Create(characterName, characterID, owner, token.RefreshToken, scope)
}
