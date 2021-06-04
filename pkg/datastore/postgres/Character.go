package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Character struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID uuid.UUID `json:"id" db:"id"`

	ProfileReference uuid.UUID `json:"profile_ref" db:"profile_ref"`

	//ESI CharacterID
	CharacterID int32 `json:"character_id" db:"character_id"`

	//ESI CharacterName
	CharacterName string `json:"name" db:"character_name"`

	//ESI CharacterOwner
	Owner string `json:"owner" db:"owner"`

	//RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" db:"refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes pgtype.TextArray `json:"scopes" db:"scopes"`

	Active bool `json:"active" db:"active"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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
	out := make([]string, 0)
	_ = c.Scopes.AssignTo(&out)
	return out
}

func (c *Character) GetProfile(ctx context.Context) (evesso.Profile, error) {
	return c.store.GetProfile(ctx, c.ProfileReference)
}

func (c *Character) UpdateToken(ctx context.Context, RefreshToken string) error {
	c.Lock()
	defer c.Unlock()
	c.RefreshToken = RefreshToken
	return c.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set refresh_token = $1, updated_at = $2 where id = $3"
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, RefreshToken, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

func (c *Character) UpdateActiveState(ctx context.Context, active bool) error {
	c.Lock()
	defer c.Unlock()
	old := c.Active
	c.Active = active
	err := c.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set active = $1, updated_at = $2 where id = $3"
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, active, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
		},
	)
	if err != nil {
		c.Active = old
		return err
	}
	return nil
}

func (c *Character) Token() (*oauth2.Token, error) {
	timeout, cancelFunc := context.WithTimeout(context.TODO(), 1*time.Minute)
	defer cancelFunc()
	tx, err := c.store.Connection(timeout)
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	q := "select refresh_token from characters where id = $1"
	err = tx.QueryRow(timeout, q, c.ID).Scan(&c.RefreshToken)
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{RefreshToken: c.RefreshToken, Expiry: time.Now()}, nil
}

func (c *Character) Delete(ctx context.Context) error {
	return c.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `DELETE FROM characters WHERE id = $1`
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, c.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

var _ evesso.Character = &Character{}
