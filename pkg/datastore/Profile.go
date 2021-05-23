package datastore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"
)

type Profile struct {
	sync.Mutex `db:"-"`
	persister  *Persister `db:"-"`

	ID string `json:"id" db:"id"`

	//ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName string `json:"profile_name" db:"profile_name"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (p *Profile) Reload(ctx context.Context) error {
	return p.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("SELECT id, profile_name, created_at, updated_at FROM profiles WHERE id = ?")
			logr.FromContextOrDiscard(ctx).Info(q, "id", p.ID)
			return tx.QueryRowxContext(ctx, q, p.ID).StructScan(p)
		},
	)
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

			query["scopes"] = Scopes
			queryParams = append(queryParams, `scopes = :scopes`)

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

func (p *Profile) Delete(ctx context.Context) error {
	return p.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("delete from profiles where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", p.ID)
			_, err := tx.ExecContext(ctx, q, p.ID)
			return err
		},
	)
}
