package datastore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	"golang.org/x/oauth2"
)

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

	//RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" db:"refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes Scopes `json:"scopes" db:"scopes"`

	Active bool `json:"active" db:"active"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (c *Character) Reload(ctx context.Context) error {
	return c.persister.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			query := make(map[string]interface{})
			queryParams := make([]string, 0)
			dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s LIMIT 1`

			if c.CharacterID > 0 {
				query["character_id"] = c.CharacterID
				queryParams = append(queryParams, `character_id = :character_i`)
			}
			if len(c.CharacterName) > 0 {
				query["character_name"] = c.CharacterName
				queryParams = append(queryParams, `character_name = :character_name`)
			}
			if len(c.Owner) > 0 {
				query["owner"] = c.Owner
				queryParams = append(queryParams, `owner = :owner`)
			}
			query["profile_ref"] = c.ProfileReference
			queryParams = append(queryParams, `profile_ref = :profile_ref`)
			query["active"] = true
			queryParams = append(queryParams, `active = :active`)

			q := fmt.Sprintf(dataQuery, strings.Join(queryParams, " AND "))
			logr.FromContextOrDiscard(ctx).Info(q, "id", c.ProfileReference, "cid", c.ID)
			namedContext, err := tx.PrepareNamedContext(ctx, q)
			if err != nil {
				return err
			}
			return namedContext.QueryRowxContext(ctx, query).StructScan(c)
		},
	)
}

func (c *Character) GetProfile(ctx context.Context) (*Profile, error) {
	return c.persister.GetProfile(ctx, c.ProfileReference)
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
		},
	)
}
