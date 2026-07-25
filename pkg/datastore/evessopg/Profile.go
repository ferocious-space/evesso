package evessopg

import (
	"context"
	"sync"
	"time"

	"github.com/goccy/go-json"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Profile struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID uuid.UUID `json:"id" db:"id"`

	// ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName string `json:"profile_name" db:"profile_name"`
	Data        []byte `json:"data" db:"data"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (p *Profile) GetData() interface{} {
	if len(p.Data) == 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(p.Data, &out); err != nil {
		return nil
	}
	return out
}

func (p *Profile) AllCharacters(ctx context.Context) (result []evesso.Character, err error) {
	var characters []*Character
	err = p.store.Query(ctx, sq.Select("*").From("evesso.characters").Where(sq.Eq{"profile_ref": p.GetID()}), &characters)
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
	q := sq.Select("*").
		From("evesso.characters").
		Where(sq.Eq{"id": uuid})
	err := p.store.Query(ctx, q, character)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) GetID() uuid.UUID {
	return p.ID
}

func (p *Profile) GetName() string {
	return p.ProfileName
}

func (p *Profile) Rename(ctx context.Context, name string) error {
	p.Lock()
	defer p.Unlock()
	old := p.ProfileName
	p.ProfileName = name
	err := p.store.Query(ctx, sq.Update("evesso.profiles").
		Set("profile_name", name).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": p.ID}), nil)
	if err != nil {
		p.ProfileName = old
		return err
	}
	return nil
}

func (p *Profile) FindCharacter(ctx context.Context, characterID int32, characterName string, owner string, scopes []string) (evesso.Character, error) {
	character := new(Character)
	character.store = p.store
	wh := sq.Select("*").From("evesso.characters")
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
	err := p.store.Query(ctx, wh.Where(and), character)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) CreateCharacter(ctx context.Context, claims evesso.CharacterClaims, token *oauth2.Token, referenceData interface{}) (evesso.Character, error) {
	marshal, err := json.Marshal(referenceData)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	character := &Character{
		store:            p.store,
		Scopes:           claims.Scopes(),
		ProfileReference: p.ID,
		CharacterID:      claims.CharacterID(),
		CharacterName:    claims.CharacterName(),
		Owner:            claims.Owner(),
		RefreshToken:     token.RefreshToken,
		Active:           true,
		AccessToken:      token.AccessToken,
		ReferenceData:    marshal,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	sqlb := sq.Insert("evesso.characters").
		Columns("profile_ref", "character_id", "character_name", "owner", "refresh_token", "scopes", "active", "access_token", "reference_data", "created_at", "updated_at").
		Values(character.ProfileReference, character.CharacterID, character.CharacterName, character.Owner, character.RefreshToken, character.Scopes, character.Active, character.AccessToken, character.ReferenceData, character.CreatedAt, character.UpdatedAt).
		Suffix("on conflict (profile_ref, character_id, character_name, owner, scopes) do update set refresh_token = excluded.refresh_token returning id")
	err = p.store.Query(ctx, sqlb, character)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (p *Profile) CreatePKCE(ctx context.Context, referenceData interface{}, scopes ...string) (evesso.PKCE, error) {
	pkce := MakePKCE(p)
	pkce.store = p.store
	marshal, err := json.Marshal(referenceData)
	if err != nil {
		return nil, err
	}
	pkce.ReferenceData = marshal
	pkce.Scopes = scopes
	sqlb := sq.Insert("evesso.pkces").
		Columns("profile_ref", "code_verifier", "code_challange", "code_challange_method", "scopes", "reference_data", "created_at").
		Values(pkce.ProfileReference, pkce.CodeVerifier, pkce.CodeChallange, pkce.CodeChallangeMethod, pkce.Scopes, pkce.ReferenceData, pkce.CreatedAt).
		Suffix("RETURNING id,state")
	err = p.store.Query(ctx, sqlb, pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (p *Profile) Delete(ctx context.Context) error {
	err := p.store.Query(ctx, sq.Delete("evesso.profiles").Where(sq.Eq{"id": p.ID}), nil)
	if err != nil {
		return err
	}
	return nil
}

var _ evesso.Profile = &Profile{}
