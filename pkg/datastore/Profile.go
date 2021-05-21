package datastore

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

var (
	ErrNoQuery = errors.New("all search parameters are nil")

	ErrTokenScope = errors.New("scope is missing")
	ErrTokenName  = errors.New("name is missing")
	ErrTokenOwner = errors.New("owner is missing")
	ErrTokenID    = errors.New("id is missing")
)

var ErrTranscationOpen = errors.New("transaction already exist in this context")
var ErrNoTranscationOpen = errors.New("no transaction in this context")

type Profile struct {
	sync.Mutex `db:"-"`
	persister  *Persister `db:"-"`

	ID string `json:"id" db:"id"`

	//ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName string    `json:"profile_name" db:"profile_name"`
	ProfileData *JSONData `json:"profile_data" db:"profile_data"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type JSONData struct {
	data interface{}
}

func NewJSONData(i interface{}) *JSONData {
	return &JSONData{i}
}

func (j *JSONData) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), &j.data)
}

func (j JSONData) Value() (driver.Value, error) {
	return json.Marshal(j.data)
}

func (p *Profile) GetCharacter(ctx context.Context, characterID int32, characterName string, Owner string, Scopes Scopes) (*Character, error) {
	character := new(Character)
	character.persister = p.persister
	return character, p.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			query := make(map[string]interface{})
			queryParams := make([]string, 0)
			dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s LIMIT 1`

			if characterID > 0 {
				query["character_id"] = characterID
				queryParams = append(queryParams, `character_id = :character_i`)
			}
			if len(characterName) > 0 {
				query["character_name"] = characterName
				queryParams = append(queryParams, `character_name = :character_name`)
			}
			if len(Owner) > 0 {
				query["owner"] = Owner
				queryParams = append(queryParams, `owner = :owner`)
			}
			query["profile_ref"] = p.ID
			queryParams = append(queryParams, `profile_ref = :profile_ref`)
			query["active"] = true
			queryParams = append(queryParams, `active = :active`)

			q := fmt.Sprintf(dataQuery, strings.Join(queryParams, " AND "))
			logr.FromContextOrDiscard(ctx).Info(q, "id", p.ID)
			namedContext, err := tx.PrepareNamedContext(ctx, q)
			if err != nil {
				return err
			}
			return namedContext.QueryRowxContext(ctx, query).StructScan(character)
		},
	)
}

func (p *Profile) CreateCharacter(ctx context.Context, token *oauth2.Token) (*Character, error) {
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
	character := &Character{
		ID:               uuid.NewString(),
		persister:        p.persister,
		ProfileReference: p.ID,
		CharacterID:      characterID,
		CharacterName:    characterName,
		Owner:            owner,
		RefreshToken:     token.RefreshToken,
		Active:           true,
		Scopes:           scope,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	return character, p.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := `INSERT INTO characters (id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at) VALUES (:id,:profile_ref,:character_id,:character_name,:owner, :refresh_token, :scopes, :active, :created_at, :updated_at)`
			logr.FromContextOrDiscard(ctx).Info(q, "id", character.ID, "profile", p.ID)
			_, err := tx.NamedExecContext(ctx, q, character)
			return err
		},
	)
}

func (p *Profile) CreatePKCE(ctx context.Context) (*PKCE, error) {
	pkce := MakePKCE(p)
	return pkce, p.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := `insert into pkces (id, profile_ref, state, code_verifier, code_challange, created_at) VALUES (:id,:profile_ref,:state,:code_verifier,:code_challange,:created_at)`
			logr.FromContextOrDiscard(ctx).Info(q, "id", pkce.ID, "profile", p.ID)
			_, err := tx.NamedExecContext(ctx, q, pkce)
			return err
		},
	)
}

type Character struct {
	sync.Mutex `db:"-"`
	persister  *Persister `db:"-"`

	ID string `json:"id" db:"id"`

	ProfileReference string `json:"profile_ref" db:"profile_ref"`

	//ESI CharacterID
	CharacterID int32 `json:"character_id" db:"character_id"`

	//ESI CharacterName
	CharacterName string `json:"name" db:"character_name"`

	//ESI CharacterOwner
	Owner string `json:"owner" db:"owner"`

	//Custom CharacterData
	CharacterData *JSONData `json:"character_data" db:"character_data"`

	//RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" db:"refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes Scopes `json:"scopes" db:"scopes"`

	Active bool `json:"active" db:"active"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (c *Character) UpdateToken(ctx context.Context, RefreshToken string) error {
	c.RefreshToken = RefreshToken
	return c.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("update characters set refresh_token = ? , updated_at = ? where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			_, err := tx.ExecContext(ctx, q, RefreshToken, time.Now(), c.ID)
			if err != nil {
				return err
			}
			return err
		},
	)
}

func (c *Character) UpdateActiveState(ctx context.Context, active bool) error {
	c.Lock()
	defer c.Unlock()
	old := c.Active
	c.Active = active
	err := c.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("update characters set active = ?, updated_at = ? where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			_, err := tx.ExecContext(ctx, q, active, time.Now(), c.ID)
			if err != nil {
				return err
			}
			return err
		},
	)
	if err != nil {
		c.Active = old
		return err
	}
	return nil
}

func (c *Character) Token() (*oauth2.Token, error) {
	return &oauth2.Token{RefreshToken: c.RefreshToken, Expiry: time.Now()}, nil
}

func (c *Character) Delete(ctx context.Context) error {
	return c.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("delete from characters where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			_, err := tx.ExecContext(ctx, q, c.ID)
			if err != nil {
				return err
			}
			return err
			//return HandleError(c.persister.Connection(ctx).Destroy(c))
		},
	)
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
