package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Profile struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID pgtype.UUID `json:"id" db:"id"`

	// ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName pgtype.Text `json:"profile_name" db:"profile_name"`

	CreatedAt pgtype.Timestamptz `json:"created_at" db:"created_at"`
	UpdatedAt pgtype.Timestamptz `json:"updated_at" db:"updated_at"`
}

func (p *Profile) AllCharacters(ctx context.Context) ([]evesso.Character, error) {
	var characters []*Character
	var result []evesso.Character //nolint:prealloc
	err := p.store.Select(ctx, sq.Select("*").From("characters").Where(sq.Eq{"profile_ref": p.GetID()}), &characters)
	if err != nil {
		return nil, err
	}
	for _, c := range characters {
		c.store = p.store
		result = append(result, c)
	}
	return result, nil
}

func (p *Profile) GetCharacter(ctx context.Context, uuid uuid.UUID) (evesso.Character, error) {
	character := new(Character)
	character.store = p.store
	rsql, args, err := sq.Select("*").
		From("characters").
		Where(sq.Eq{"id": uuid}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, err
	}
	logr.FromContextOrDiscard(ctx).Info(rsql, "id", uuid.String())
	err = p.store.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
		return pgxscan.Get(ctx, tx, character, rsql, args...)
	})
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) GetID() uuid.UUID {
	var uid []byte
	if err := p.ID.AssignTo(&uid); err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(uid)
}

func (p *Profile) GetName() string {
	name := ""
	_ = p.ProfileName.AssignTo(&name)
	return name
}

func (p *Profile) FindCharacter(ctx context.Context, characterID int32, characterName string, owner string, scopes []string) (evesso.Character, error) {
	character := new(Character)
	character.store = p.store
	wh := sq.Select("*").From("characters")
	and := sq.And{}
	if characterID > 0 {
		and = append(and, sq.Eq{"character_id": characterID})
	}
	if len(characterName) > 0 {
		and = append(and, sq.Eq{"character_name": characterName})
	}
	if len(owner) > 0 {
		and = append(and, sq.Eq{"owner": owner})
	}
	and = append(and, sq.Eq{"profile_ref": p.ID})
	and = append(and, sq.Expr("scopes @> (?)", scopes))
	and = append(and, sq.Eq{"active": true})
	err := p.store.Select(ctx, wh.Where(and), character)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) CreateCharacter(ctx context.Context, token *oauth2.Token, referenceData interface{}) (evesso.Character, error) {
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

	switch x := scp.(type) {
	case string:
		scope = append([]string{}, x)
	default:
		for k := range scp.([]interface{}) {
			scope = append(scope, scp.([]interface{})[k].(string))
		}
	}

	CharacterName, ok := jToken.Get("name")
	if !ok {
		return nil, ErrTokenName
	}
	characterName = CharacterName.(string)
	Owner, ok := jToken.Get("owner")
	if !ok {
		return nil, ErrTokenOwner
	}
	owner = Owner.(string)

	subj := jToken.Subject()
	if n, err := fmt.Sscanf(subj, "CHARACTER:EVE:%d", &characterID); err != nil || n != 1 {
		return nil, ErrTokenID
	}
	sort.Strings(scope)
	character := &Character{
		store:  p.store,
		Scopes: MakeScopes(scope),
	}
	marshal, err := json.Marshal(referenceData)
	if err != nil {
		return nil, err
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
	if err := character.AccessToken.Set(token.AccessToken); err != nil {
		return nil, err
	}
	if err := character.ReferenceData.Set(marshal); err != nil {
		return nil, err
	}
	sqlb := sq.Insert("characters").
		Columns("profile_ref",
			"character_id",
			"character_name",
			"owner",
			"refresh_token",
			"scopes",
			"active",
			"created_at",
			"updated_at",
			"access_token",
			"reference_data").
		Values(&character.ProfileReference,
			&character.CharacterID,
			&character.CharacterName,
			&character.Owner,
			&character.RefreshToken,
			&character.Scopes,
			&character.Active,
			&character.CreatedAt,
			&character.UpdatedAt,
			&character.AccessToken,
			&character.ReferenceData).
		Suffix("on conflict (profile_ref, character_id, character_name, owner, scopes) do update set refresh_token = excluded.refresh_token returning id")
	err = p.store.Select(ctx, sqlb, character)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) CreatePKCE(ctx context.Context, referenceData interface{}, scopes ...string) (evesso.PKCE, error) {
	pkce := MakePKCE(p)
	pkce.store = p.store
	rdata := pgtype.JSONB{}
	marshal, err := json.Marshal(referenceData)
	if err != nil {
		return nil, err
	}
	err = rdata.Set(marshal)
	if err != nil {
		return nil, err
	}
	err = p.store.Select(ctx, sq.Insert("pkces").
		Columns("profile_ref", "code_verifier", "code_challange", "scopes", "created_at", "reference_data").
		Values(pkce.ProfileReference, pkce.CodeVerifier, pkce.CodeChallange, MakeScopes(scopes), pkce.CreatedAt, rdata).
		Suffix("RETURNING id,state"), pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (p *Profile) Delete(ctx context.Context) error {
	err := p.store.Exec(ctx, sq.Delete("profiles").Where("id = ?", p.ID))
	if err != nil {
		return err
	}
	return nil
}

var _ evesso.Profile = &Profile{}
