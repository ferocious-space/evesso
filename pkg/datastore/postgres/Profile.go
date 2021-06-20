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
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Profile struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID pgtype.UUID `json:"id" db:"id"`

	//ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName pgtype.Text `json:"profile_name" db:"profile_name"`

	CreatedAt pgtype.Timestamptz `json:"created_at" db:"created_at"`
	UpdatedAt pgtype.Timestamptz `json:"updated_at" db:"updated_at"`
}

func (p *Profile) AllCharacters(ctx context.Context) ([]evesso.Character, error) {
	result := make([]evesso.Character, 0)
	ids := make([]uuid.UUID, 0)
	dataQuery := `SELECT id FROM characters where profile_ref = $1`
	tx, err := p.store.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	rows, err := tx.Query(ctx, dataQuery, p.GetID())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var uid uuid.UUID
		err := rows.Scan(&uid)
		if err != nil {
			return nil, err
		}
		ids = append(ids, uid)
	}
	defer rows.Close()
	for _, uid := range ids {
		ch, err := p.GetCharacter(ctx, uid)
		if err != nil {
			return nil, err
		}
		result = append(result, ch)
	}
	return result, nil
}

func (p *Profile) GetCharacter(ctx context.Context, uuid uuid.UUID) (evesso.Character, error) {
	character := new(Character)
	character.store = p.store
	tx, err := p.store.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE id = $1`
	return character, HandleError(
		tx.QueryRow(ctx, dataQuery, uuid).Scan(
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
		),
	)
}

func (p *Profile) GetID() uuid.UUID {
	uid := []byte{}
	err := p.ID.AssignTo(&uid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(uid)
}

func (p *Profile) GetName() string {
	name := ""
	_ = p.ProfileName.AssignTo(&name)
	return name
}
func (p *Profile) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string, Scopes []string) (evesso.Character, error) {
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

	return character, HandleError(
		tx.QueryRow(ctx, q, queryParams...).Scan(
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
		),
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
		store:  p.store,
		Scopes: MakeScopes(scope),
	}
	if err := character.CreatedAt.Set(time.Now()); err != nil {
		return nil, err
	}
	if err := character.UpdatedAt.Set(time.Now()); err != nil {
		return nil, err
	}
	if err := character.ProfileReference.Set(p.ID); err != nil {
		return nil, err
	}
	if err := character.CharacterID.Set(characterID); err != nil {
		return nil, err
	}
	if err := character.CharacterName.Set(characterName); err != nil {
		return nil, err
	}
	if err := character.Owner.Set(owner); err != nil {
		return nil, err
	}
	if err := character.RefreshToken.Set(token.RefreshToken); err != nil {
		return nil, err
	}
	if err := character.Active.Set(true); err != nil {
		return nil, err
	}
	return character, p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `INSERT INTO characters (profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) returning id`
			logr.FromContextOrDiscard(ctx).Info(q, "id", character.ID, "profile", p.ID)
			if err := tx.QueryRow(
				ctx, q,
				&character.ProfileReference,
				&character.CharacterID,
				&character.CharacterName,
				&character.Owner,
				&character.RefreshToken,
				&character.Scopes,
				&character.Active,
				&character.CreatedAt,
				&character.UpdatedAt,
			).Scan(&character.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

func (p *Profile) CreatePKCE(ctx context.Context, scopes ...string) (evesso.PKCE, error) {
	pkce := MakePKCE(p)
	return pkce, p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `insert into pkces (profile_ref, code_verifier, code_challange, scopes, created_at) VALUES ($1,$2,$3,$4,$5) returning id,state`
			logr.FromContextOrDiscard(ctx).Info(q, "id", pkce.ID, "profile", p.ID)
			if err := tx.QueryRow(ctx, q, pkce.ProfileReference, pkce.CodeVerifier, pkce.CodeChallange, MakeScopes(scopes), pkce.CreatedAt).Scan(
				&pkce.ID,
				&pkce.State,
			); err != nil {
				return err
			}
			return nil
		},
	)
}

func (p *Profile) Delete(ctx context.Context) error {
	return p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "DELETE FROM profiles WHERE id = $1"
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", p.ID)
			if _, err := tx.Exec(ctx, q, p.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

var _ evesso.Profile = &Profile{}
