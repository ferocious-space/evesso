package evessopg

import (
	"context"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Character struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID uuid.UUID `json:"id" db:"id"`

	ProfileReference uuid.UUID `json:"profile_ref" db:"profile_ref"`

	// ESI CharacterID
	CharacterID int32 `json:"character_id" db:"character_id"`

	// ESI CharacterName
	CharacterName string `json:"name" db:"character_name"`

	// ESI CharacterOwner
	Owner string `json:"owner" db:"owner"`

	// Last issued oauth2 AccessToken
	AccessToken string `json:"access_token" db:"access_token"`

	// RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" db:"refresh_token"`

	// Scopes is the scopes the refresh token was issued with
	Scopes []string `json:"scopes" db:"scopes"`

	// ReferenceData is custom data passed during authentication
	ReferenceData []byte `json:"reference_data" db:"reference_data"`

	Active bool `json:"active" db:"active"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (c *Character) GetReferenceData() interface{} {
	if len(c.ReferenceData) == 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(c.ReferenceData, &out); err != nil {
		return nil
	}
	return out
}

func (c *Character) UpdateAccessToken(ctx context.Context, accessToken string) error {
	c.Lock()
	defer c.Unlock()
	// c.store.GLock(c.CharacterID.Get())
	//defer c.store.GUnlock(c.CharacterID.Get())
	c.AccessToken = accessToken
	err := c.store.Query(ctx, sq.Update("evesso.characters").
		Set("access_token", c.AccessToken).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": c.ID}), nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *Character) GetID() uuid.UUID {
	return c.ID
}

func (c *Character) GetCharacterName() string {
	return c.CharacterName
}

func (c *Character) GetCharacterID() int32 {
	return c.CharacterID
}

func (c *Character) GetOwner() string {
	return c.Owner
}

func (c *Character) IsActive() bool {
	return c.Active
}

func (c *Character) GetProfileID() uuid.UUID {
	return c.ProfileReference
}

func (c *Character) GetScopes() []string {
	return c.Scopes
}

func (c *Character) GetProfile(ctx context.Context) (evesso.Profile, error) {
	return c.store.GetProfile(ctx, c.GetProfileID())
}

func (c *Character) UpdateRefreshToken(ctx context.Context, refreshToken string) error {
	c.Lock()
	defer c.Unlock()
	c.RefreshToken = refreshToken
	err := c.store.Query(ctx, sq.Update("evesso.characters").
		Set("refresh_token", c.RefreshToken).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": c.ID}), nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *Character) UpdateActiveState(ctx context.Context, active bool) error {
	c.Lock()
	defer c.Unlock()
	old := c.Active
	c.Active = active
	err := c.store.Query(ctx, sq.Update("evesso.characters").Set("active", c.Active).Set("updated_at", time.Now()).Where(sq.Eq{"id": c.ID}), nil)
	if err != nil {
		c.Active = old
		return err
	}
	return nil
}

func (c *Character) Token() (*oauth2.Token, error) {
	c.Lock()
	defer c.Unlock()
	timeout, cancelFunc := context.WithTimeout(context.TODO(), time.Minute)
	defer cancelFunc()
	err := c.store.Query(timeout,
		sq.Select("access_token", "refresh_token").
			From("evesso.characters").
			Where(sq.Eq{"id": c.ID}),
		c)
	if err != nil {
		return nil, err
	}
	refreshToken := c.RefreshToken
	expiration := time.Now().UTC()
	accessToken := c.AccessToken
	if len(accessToken) > 1 {
		// ParseInsecure is deliberate here: this reads back a token this process
		// wrote, and the expiry only decides whether oauth2 refreshes now or in a
		// moment. Nothing is trusted on the strength of it — identity comes from
		// evesso.CharacterClaims, and ssoTokenSource.validate verifies any token
		// that is actually used. A JWKS lookup on every DB read would buy nothing.
		parseString, parseErr := jwt.ParseInsecure([]byte(accessToken))
		if parseErr != nil {
			accessToken = ""
		} else if exp, ok := parseString.Expiration(); ok {
			expiration = exp
		}
	}
	return &oauth2.Token{AccessToken: accessToken, RefreshToken: refreshToken, Expiry: expiration}, nil
}

func (c *Character) Delete(ctx context.Context) error {
	err := c.store.Query(ctx, sq.Delete("evesso.characters").Where(sq.Eq{"id": c.ID}), nil)
	if err != nil {
		return err
	}
	return nil
}

var _ evesso.Character = &Character{}
