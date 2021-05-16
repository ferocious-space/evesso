package datastore

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

var (
	ErrNoQuery = errors.New("all search parameters are nil")

	ErrTokenScope = errors.New("scope is missing")
	ErrTokenName  = errors.New("name is missing")
	ErrTokenOwner = errors.New("owner is missing")
	ErrTokenID    = errors.New("id is missing")

	ErrProfileExists   = errors.New("profile already exists")
	ErrProfileNotFound = errors.New("profile not found")

	ErrCharacterNoProfile = errors.New("character has no profile")
	ErrCharacterExists    = errors.New("character already exists")
	ErrCharacterNotFound  = errors.New("character not found")
)

type Profile struct {
	store DataStore `gorm:"-"`

	ID uuid.UUID `gorm:"primaryKey"`
	//ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName string     `json:"profile_name" gorm:"uniqueIndex;column:profile_name"`
	ProfileData []byte     `json:"profile_data" gorm:"column:profile_data;"`
	Characters  Characters `json:"characters"`
}

func (p *Profile) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

func (p *Profile) Update(ProfileName string, ProfileData interface{}) error {
	updates := map[string]interface{}{}
	if ProfileName != "" {
		updates["profile_name"] = ProfileName
	}
	if ProfileData != nil {
		marshal, err := json.Marshal(ProfileData)
		if err != nil {
			return err
		}
		updates["profile_data"] = marshal
	}
	return p.store.gdb().Model(p).Updates(updates).Error
}

func (p *Profile) CreateCharacter(character *Character) error {
	return p.store.CreateCharacter(p.ID, p.ProfileName, character)
}

func (p *Profile) DeleteCharacter(character *Character) error {
	return p.store.DeleteCharacter(p.ID, p.ProfileName, character)
}

func (p *Profile) MakePKCE() (*PKCE, error) {
	return p.store.CreatePKCE(p)
}

type Characters []*Character
type Character struct {
	store DataStore `gorm:"-"`

	ProfileID uuid.UUID `json:"profile_id" gorm:"type:uuid;column:profile_id;primaryKey;"`

	//ESI CharacterID
	CharacterID int32 `json:"character_id" gorm:"primaryKey;index;check:character_id > 0;column:character_id;"`

	//ESI CharacterName
	CharacterName string `json:"name" gorm:"index;column:character_name;primaryKey;"`

	//ESI CharacterOwner
	Owner string `json:"owner" gorm:"index;column:owner;primaryKey;"`

	//Custom CharacterData
	CharacterData []byte `json:"character_data" gorm:"column:character_data;"`

	//RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" gorm:"column:refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes Scopes `json:"scopes" gorm:"primaryKey;index;"`
}

func (c *Character) Find(store DataStore, profileID uuid.UUID, scopes Scopes) (*Character, error) {
	return store.FindCharacter(profileID, c.CharacterID, c.CharacterName, c.Owner, scopes)
}

func (c *Character) Update(RefreshToken string, CharacterData interface{}) error {
	updates := map[string]interface{}{}
	if RefreshToken != "" {
		updates["refresh_token"] = RefreshToken
	}
	if CharacterData != nil {
		marshal, err := json.Marshal(CharacterData)
		if err != nil {
			return err
		}
		updates["character_data"] = marshal
	}
	return c.store.gdb().Model(c).Updates(updates).Error
}

func WithScopes(scopes ...string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		s := scopes[:]
		sort.Strings(s)
		marshal, err := json.Marshal(s)
		if err != nil {
			return db
		}
		return db.Where("scopes = ?", marshal)
	}
}

func (c *Character) Token() *oauth2.Token {
	return &oauth2.Token{RefreshToken: c.RefreshToken, Expiry: time.Now()}
}

type Scopes []string

func (s Scopes) Value() (driver.Value, error) {
	scp := s[:]
	sort.Strings(scp)
	return json.Marshal(scp)
}

func (s *Scopes) Scan(src interface{}) error {
	data, ok := src.([]byte)
	if !ok {
		return errors.Errorf("unable to unmarshal Scopes value: %v", src)
	}
	return json.Unmarshal(data, &s)
}

type DecodeFN func(data []byte, v interface{}) error

func FromLocation(location io.Reader, fn DecodeFN) (*Character, error) {
	token := new(oauth2.Token)
	buf := new(bytes.Buffer)
	_, err := io.Copy(buf, location)
	if err != nil {
		return nil, err
	}
	err = fn(buf.Bytes(), &token)
	if err != nil {
		return nil, err
	}
	return ParseToken(token)
}

//ParseToken gets oauth2.Token and maps it to Character and CharacterToken structures
//resulting structs dont have keys populated
func ParseToken(token *oauth2.Token) (*Character, error) {
	jToken, err := jwt.Parse([]byte(token.AccessToken))
	if err != nil {
		return nil, err
	}

	var characterName, owner string
	var characterID int32
	var scope []string

	scp, ok := jToken.Get("scp")
	if !ok {
		return nil, ErrTokenScope
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
		return nil, ErrTokenName
	} else {
		characterName = CharacterName.(string)
	}
	if Owner, ok := jToken.Get("owner"); !ok {
		return nil, ErrTokenOwner
	} else {
		owner = Owner.(string)
	}

	subj := jToken.Subject()
	if n, err := fmt.Sscanf(subj, "CHARACTER:EVE:%d", &characterID); err != nil || n != 1 {
		return nil, ErrTokenID
	}
	sort.Strings(scope)
	char := &Character{
		CharacterID:   characterID,
		CharacterName: characterName,
		Owner:         owner,
		RefreshToken:  token.RefreshToken,
		Scopes:        scope,
	}
	return char, nil
}
