package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/jackc/pgx/v4"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso"
)

type Character struct {
	sync.Mutex `db:"-"`
	persister  *PGStore `db:"-"`

	ID string `json:"id" db:"id"`

	ProfileReference evesso.ProfileID `json:"profile_ref" db:"profile_ref"`

	//ESI CharacterID
	CharacterID int32 `json:"character_id" db:"character_id"`

	//ESI CharacterName
	CharacterName string `json:"name" db:"character_name"`

	//ESI CharacterOwner
	Owner string `json:"owner" db:"owner"`

	//RefreshToken is oauth2 refresh token
	RefreshToken string `json:"refresh_token" db:"refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes Scope `json:"scopes" db:"scopes"`

	Active bool `json:"active" db:"active"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (c *Character) GetID() string {
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

func (c *Character) GetProfileID() evesso.ProfileID {
	return evesso.ProfileID(c.ProfileReference)
}

func (c *Character) GetScopes() []string {
	return c.Scopes.Get()
}

//func (c *Character) Reload(ctx context.Context) error {
//	return c.persister.transaction(
//		ctx, func(ctx context.Context, tx pgx.Tx) error {
//			query := make(map[string]interface{})
//			queryParams := make([]string, 0)
//			dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s LIMIT 1`
//
//			if c.CharacterID > 0 {
//				query["character_id"] = c.CharacterID
//				queryParams = append(queryParams, `character_id = :character_i`)
//			}
//			if len(c.CharacterName) > 0 {
//				query["character_name"] = c.CharacterName
//				queryParams = append(queryParams, `character_name = :character_name`)
//			}
//			if len(c.Owner) > 0 {
//				query["owner"] = c.Owner
//				queryParams = append(queryParams, `owner = :owner`)
//			}
//			query["profile_ref"] = c.ProfileReference
//			queryParams = append(queryParams, `profile_ref = :profile_ref`)
//			query["active"] = true
//			queryParams = append(queryParams, `active = :active`)
//
//			q := fmt.Sprintf(dataQuery, strings.Join(queryParams, " AND "))
//			logr.FromContextOrDiscard(ctx).Info(q, "id", c.ProfileReference, "cid", c.ID)
//			namedContext, err := tx.PrepareNamedContext(ctx, q)
//			if err != nil {
//				return err
//			}
//			return namedContext.QueryRowxContext(ctx, query).StructScan(c)
//		},
//	)
//}

func (c *Character) GetProfile(ctx context.Context) (evesso.Profile, error) {
	return c.persister.GetProfile(ctx, evesso.ProfileID(c.ProfileReference))
}

func (c *Character) UpdateToken(ctx context.Context, RefreshToken string) error {
	c.RefreshToken = RefreshToken
	return c.persister.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set refresh_token = $1, updated_at = $2 where id = $3"
			//q := tx.Rebind("update characters set refresh_token = ? , updated_at = ? where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, RefreshToken, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
			//_, err := tx.ExecContext(ctx, q, RefreshToken, time.Now(), c.ID)
			//if err != nil {
			//	return err
			//}
			//return err
		},
	)
}

func (c *Character) UpdateActiveState(ctx context.Context, active bool) error {
	c.Lock()
	defer c.Unlock()
	old := c.Active
	c.Active = active
	err := c.persister.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set active = $1, updated_at = $2 where id = $3"
			//q := tx.Rebind("update characters set active = ?, updated_at = ? where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, active, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
			//_, err := tx.ExecContext(ctx, q, active, time.Now(), c.ID)
			//if err != nil {
			//	return err
			//}
			//return err
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
	return c.persister.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `DELETE FROM characters WHERE id = $1`
			//q := tx.Rebind("delete from characters where id = ?")
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, c.ID); err != nil {
				return err
			}
			//_, err := tx.ExecContext(ctx, q, c.ID)
			//if err != nil {
			//	return err
			//}
			//return err
			return nil
		},
	)
}

var _ evesso.Character = &Character{}
