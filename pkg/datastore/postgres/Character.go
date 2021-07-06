package postgres

import (
	"context"
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

type Character struct {
	sync.Mutex `db:"-"`
	store      *PGStore `db:"-"`

	ID pgtype.UUID `json:"id" db:"id"`

	ProfileReference pgtype.UUID `json:"profile_ref" db:"profile_ref"`

	//ESI CharacterID
	CharacterID pgtype.Int4 `json:"character_id" db:"character_id"`

	//ESI CharacterName
	CharacterName pgtype.Text `json:"name" db:"character_name"`

	//ESI CharacterOwner
	Owner pgtype.Text `json:"owner" db:"owner"`

	//Last issued oauth2 AccessToken
	AccessToken pgtype.Text `json:"access_token" db:"access_token"`

	//RefreshToken is oauth2 refresh token
	RefreshToken pgtype.Text `json:"refresh_token" db:"refresh_token"`

	//Scopes is the scopes the refresh token was issued with
	Scopes pgtype.TextArray `json:"scopes" db:"scopes"`

	//ReferenceData is custom data passed during authentication
	ReferenceData pgtype.JSONB `json:"reference_data" db:"reference_data"`

	Active pgtype.Bool `json:"active" db:"active"`

	CreatedAt pgtype.Timestamptz `json:"created_at" db:"created_at"`
	UpdatedAt pgtype.Timestamptz `json:"updated_at" db:"updated_at"`
}

func (c *Character) GetReferenceData() interface{} {
	return c.ReferenceData.Get()
}

func (c *Character) UpdateAccessToken(ctx context.Context, AccessToken string) error {
	c.Lock()
	defer c.Unlock()
	err := c.AccessToken.Set(AccessToken)
	if err != nil {
		return err
	}
	return c.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set access_token = $1, updated_at = $2 where id = $3"
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, c.AccessToken, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

func (c *Character) GetID() uuid.UUID {
	cid := []byte{}
	err := c.ID.AssignTo(&cid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(cid)
}

func (c *Character) GetCharacterName() string {
	name := ""
	_ = c.CharacterName.AssignTo(&name)
	return name
}

func (c *Character) GetCharacterID() int32 {
	id := int32(0)
	_ = c.CharacterID.AssignTo(&id)
	return id
}

func (c *Character) GetOwner() string {
	owner := ""
	_ = c.Owner.AssignTo(&owner)
	return owner
}

func (c *Character) IsActive() bool {
	active := false
	_ = c.Active.AssignTo(&active)
	return active
}

func (c *Character) GetProfileID() uuid.UUID {
	cid := []byte{}
	err := c.ProfileReference.AssignTo(&cid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(cid)
}

func (c *Character) GetScopes() []string {
	out := make([]string, 0)
	_ = c.Scopes.AssignTo(&out)
	return out
}

func (c *Character) GetProfile(ctx context.Context) (evesso.Profile, error) {
	return c.store.GetProfile(ctx, c.GetProfileID())
}

func (c *Character) UpdateRefreshToken(ctx context.Context, RefreshToken string) error {
	c.Lock()
	defer c.Unlock()
	err := c.RefreshToken.Set(RefreshToken)
	if err != nil {
		return err
	}
	return c.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "update characters set refresh_token = $1, updated_at = $2 where id = $3"
			logr.FromContextOrDiscard(ctx).V(1).Info(q, "id", c.ID, "profile", c.ProfileReference)
			if _, err := tx.Exec(ctx, q, c.RefreshToken, time.Now(), c.ID); err != nil {
				return err
			}
			return nil
		},
	)
}

func (c *Character) UpdateActiveState(ctx context.Context, active bool) error {
	c.Lock()
	defer c.Unlock()
	old := false
	err := c.Active.AssignTo(&old)
	if err != nil {
		return err
	}
	err = c.Active.Set(active)
	if err != nil {
		return err
	}
	err = c.store.transaction(
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
		err := c.Active.Set(old)
		if err != nil {
			return err
		}
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
	q := "select access_token,refresh_token from characters where id = $1"
	err = tx.QueryRow(timeout, q, c.ID).Scan(&c.AccessToken, &c.RefreshToken)
	if err != nil {
		return nil, HandleError(err)
	}
	refreshToken := ""
	if err := c.RefreshToken.AssignTo(&refreshToken); err != nil {
		return nil, err
	}
	expiration := time.Now().UTC()
	accessToken := ""
	_ = c.AccessToken.AssignTo(&accessToken)
	if len(accessToken) > 1 {
		parseString, err := jwt.ParseString(accessToken)
		if err != nil {
			accessToken = ""
		}
		expiration = parseString.Expiration()
	}
	return &oauth2.Token{AccessToken: accessToken, RefreshToken: refreshToken, Expiry: expiration}, nil
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
