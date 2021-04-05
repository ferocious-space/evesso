package datastore

import (
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"
)

type DataStore struct {
	astore AccountStore
}

func NewDataStore(astore AccountStore) *DataStore {
	return &DataStore{astore: astore}
}

func (ds *DataStore) GetToken(CharacterName string, Scopes ...string) (characterID int32, token *oauth2.Token, err error) {
	data, err := ds.astore.SearchName(CharacterName, Scopes)
	if err != nil {
		return 0, nil, err
	}
	return data.CharacterId, &oauth2.Token{
		RefreshToken: data.RefreshToken,
		Expiry:       time.Now(),
	}, nil
}

func (ds *DataStore) SaveToken(token *oauth2.Token) (CharacterID int32, err error) {
	jToken, err := jwt.Parse([]byte(token.AccessToken))
	if err != nil {
		return 0, err
	}

	var characterName, owner string
	var characterID int32
	var scope []string

	scp, ok := jToken.Get("scp")
	if !ok {
		return 0, ErrScope
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
		return 0, ErrCharacterName
	} else {
		characterName = CharacterName.(string)
	}
	if Owner, ok := jToken.Get("owner"); !ok {
		return 0, ErrCharacterName
	} else {
		owner = Owner.(string)
	}

	subj := jToken.Subject()
	if n, err := fmt.Sscanf(subj, "CHARACTER:EVE:%d", &characterID); err != nil || n != 1 {
		return 0, ErrCharacterID
	}

	return characterID, ds.astore.Create(&AccountData{
		CharacterName: characterName,
		CharacterId:   characterID,
		Owner:         owner,
		RefreshToken:  token.RefreshToken,
		Scopes:        scope,
	})
}
