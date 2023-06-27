package evessopg

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/goccy/go-json"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgtype"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Profile struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID pgtype.UUID `json:"id" db:"id"`

	// ProfileType can be used to define custom profile types , e.g. service bot that uses multiple characters to query esi for information
	ProfileName pgtype.Text  `json:"profile_name" db:"profile_name"`
	Data        pgtype.JSONB `json:"data" db:"data"`

	CreatedAt pgtype.Timestamptz `json:"created_at" db:"created_at"`
	UpdatedAt pgtype.Timestamptz `json:"updated_at" db:"updated_at"`
}

func (p *Profile) GetData() interface{} {
	var out interface{}
	err := p.Data.AssignTo(&out)
	if err != nil {
		return nil
	}
	return out
}

func (p *Profile) AllCharacters(ctx context.Context) ([]evesso.Character, error) {
	var characters []*Character
	var result []evesso.Character //nolint:prealloc
	err := p.store.Query(ctx, sq.Select("*").From("evesso.characters").Where(sq.Eq{"profile_ref": p.GetID()}), &characters)
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

func InsertGenerate(into string, input interface{}) sq.InsertBuilder {
	columns := make([]string, 0)
	values := make([]interface{}, 0)
	typ := reflect.TypeOf(input)
	val := reflect.ValueOf(input)
	switch typ.Kind() {
	case reflect.Ptr:
		typ = typ.Elem()
		val = val.Elem()
	case reflect.Struct:
	default:
		panic("input must be a struct")
	}

	for f := 0; f < typ.NumField(); f++ {
		field := typ.Field(f)
		lookup, ok := field.Tag.Lookup("db")
		if ok && lookup != "-" {
			fieldVal := val.Field(f)
			if fieldVal.Kind() == reflect.Ptr {
				fieldVal = fieldVal.Elem()
			}
			switch fieldVal.Kind() {
			case reflect.Struct:
				status := fieldVal.FieldByName("Status")
				targetType := reflect.TypeOf(pgtype.Present)
				if status.IsValid() && !status.IsZero() && status.CanInterface() && status.CanConvert(targetType) {
					if status.Convert(targetType).Interface() == pgtype.Present {
						columns = append(columns, lookup)
						values = append(values, fieldVal.Interface())
					}
				}
			default:
				columns = append(columns, lookup)
				values = append(values, fieldVal.Interface())
			}
		}
	}
	return sq.Insert(into).Columns(columns...).Values(values...)
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
	sqlb := InsertGenerate("evesso.characters", character).
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
	err = pkce.ReferenceData.Set(marshal)
	if err != nil {
		return nil, err
	}
	pkce.Scopes = MakeScopes(scopes)
	err = p.store.Query(ctx, InsertGenerate("evesso.pkces", pkce).Suffix("RETURNING id,state"), pkce)
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
