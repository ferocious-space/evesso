package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Profile struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID uuid.UUID `json:"id" db:"id"`

	//ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName string `json:"profile_name" db:"profile_name"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (p *Profile) GetID() uuid.UUID {
	return p.ID
}

func (p *Profile) GetName() string {
	return p.ProfileName
}
func (p *Profile) GetCharacter(ctx context.Context, characterID int32, characterName string, Owner string, Scopes []string) (evesso.Character, error) {
	character := new(Character)
	character.store = p.store
	tx, err := p.store.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()

	dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s`
	whereParams := []string{}
	queryParams := []interface{}{}
	counter := 0

	if characterID > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("character_id = $%d", counter))
		queryParams = append(queryParams, characterID)
	}

	if len(characterName) > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("character_name = $%d", counter))
		queryParams = append(queryParams, characterName)
	}

	if len(Owner) > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("owner = $%d", counter))
		queryParams = append(queryParams, Owner)
	}

	counter++
	whereParams = append(whereParams, fmt.Sprintf("profile_ref = $%d", counter))
	queryParams = append(queryParams, p.ID)
	counter++
	whereParams = append(whereParams, fmt.Sprintf("scopes @> ($%d)", counter))
	queryParams = append(queryParams, MakeScopes(Scopes))
	counter++
	whereParams = append(whereParams, fmt.Sprintf("active = $%d", counter))
	queryParams = append(queryParams, true)

	q := fmt.Sprintf(dataQuery, strings.Join(whereParams, " AND "))
	logr.FromContextOrDiscard(ctx).Info(q)

	return character, tx.QueryRow(ctx, q, queryParams...).Scan(
		&character.ID,
		&character.ProfileReference,
		&character.CharacterID,
		&character.CharacterName,
		&character.Owner,
		&character.RefreshToken,
		&character.Scopes,
		&character.Active,
		&character.CreatedAt,
		&character.UpdatedAt,
	)

}

func (p *Profile) CreateCharacter(ctx context.Context, token *oauth2.Token) (evesso.Character, error) {
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
		ID:               uuid.NewV5(p.ID, characterName),
		store:            p.store,
		ProfileReference: p.ID,
		CharacterID:      characterID,
		CharacterName:    characterName,
		Owner:            owner,
		RefreshToken:     token.RefreshToken,
		Active:           true,
		Scopes:           MakeScopes(scope),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	return character, p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `INSERT INTO characters (id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
			logr.FromContextOrDiscard(ctx).Info(q, "id", character.ID, "profile", p.ID)
			if _, err := tx.Exec(
				ctx, q,
				&character.ID,
				&character.ProfileReference,
				&character.CharacterID,
				&character.CharacterName,
				&character.Owner,
				&character.RefreshToken,
				&character.Scopes,
				&character.Active,
				&character.CreatedAt,
				&character.UpdatedAt,
			); err != nil {
				return err
			}
			return nil
		},
	)
}

func (p *Profile) CreatePKCE(ctx context.Context) (evesso.PKCE, error) {
	pkce := MakePKCE(p)
	return pkce, p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `insert into pkces (id, profile_ref, state, code_verifier, code_challange, created_at) VALUES ($1,$2,$3,$4,$5,$6)`
			logr.FromContextOrDiscard(ctx).Info(q, "id", pkce.ID, "profile", p.ID)
			if _, err := tx.Exec(ctx, q, pkce.ID, pkce.ProfileReference, pkce.State, pkce.CodeVerifier, pkce.CodeChallange, pkce.CreatedAt); err != nil {
				return err
			}
			return nil
			//_, err := tx.NamedExecContext(ctx, q, pkce)
			//return err
		},
	)
}

func (p *Profile) Delete(ctx context.Context) error {
	return p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "DELETE FROM profiles WHERE id = $1"
			//q := tx.Rebind("delete from profiles where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", p.ID)
			//_, err := tx.ExecContext(ctx, q, p.ID)
			//return err
			if _, err := tx.Exec(ctx, q, p.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

var _ evesso.Profile = &Profile{}
