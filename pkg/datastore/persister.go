package datastore

import (
	"context"
	"database/sql"
	"embed"
	"time"

	"github.com/gobuffalo/pop/v5"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"

	"github.com/ferocious-space/evesso/pkg/migrationbox"
)

const transactionSSOKey = "transactionSSOKey"

//go:embed migrations/*
var migrations embed.FS

type Transactional interface {
	BeginTX(ctx context.Context) (context.Context, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

func MaybeBeginTx(ctx context.Context, storage interface{}) (context.Context, error) {
	switch store := storage.(type) {
	case Transactional:
		return store.BeginTX(ctx)
	default:
		return ctx, nil
	}
}

func MaybeCommitTx(ctx context.Context, storage interface{}) error {
	switch store := storage.(type) {
	case Transactional:
		return store.Commit(ctx)
	default:
		return nil
	}
}

func MaybeRollackTx(ctx context.Context, storage interface{}) error {
	switch store := storage.(type) {
	case Transactional:
		return store.Rollback(ctx)
	default:
		return nil
	}
}

type Persister struct {
	conn *pop.Connection
	mb   *pop.Migrator
}

func (x *Persister) BeginTX(ctx context.Context) (context.Context, error) {
	_, ok := ctx.Value(transactionSSOKey).(*pop.Connection)
	if ok {
		return ctx, ErrTranscationOpen
	}
	tx, err := x.conn.Store.TransactionContextOptions(
		ctx, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  false,
		},
	)
	c := &pop.Connection{
		TX:      tx,
		Store:   tx,
		ID:      uuid.Must(uuid.NewV4()).String(),
		Dialect: x.conn.Dialect,
	}
	return context.WithValue(ctx, transactionSSOKey, c), err
}
func (x *Persister) Commit(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(*pop.Connection)
	if !ok || c.TX == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.TX.Commit()
}
func (x *Persister) Rollback(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(*pop.Connection)
	if !ok || c.TX == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.TX.Rollback()
}
func (x *Persister) Connection(ctx context.Context) *pop.Connection {
	if c, ok := ctx.Value(transactionSSOKey).(*pop.Connection); ok {
		return c.WithContext(ctx)
	}
	return x.conn.WithContext(ctx)
}

func (x *Persister) tx(ctx context.Context, f func(ctx context.Context, c *pop.Connection) error) error {
	var err error
	isNested := true
	c, ok := ctx.Value(transactionSSOKey).(*pop.Connection)
	if !ok {
		isNested = false
		c, err = x.conn.WithContext(ctx).NewTransaction()
		if err != nil {
			return errors.WithStack(err)
		}
	}

	if err := f(context.WithValue(ctx, transactionSSOKey, c), c); err != nil {
		if !isNested {
			if err := c.TX.Rollback(); err != nil {
				return errors.WithStack(err)
			}
		}
		return err
	}
	if !isNested {
		return errors.WithStack(c.TX.Commit())
	}
	return nil
}

func NewPersister(dsn string, migrate bool) (*Persister, error) {
	c, err := pop.NewConnection(
		&pop.ConnectionDetails{
			URL:             dsn,
			Pool:            50,
			ConnMaxLifetime: 300,
			ConnMaxIdleTime: 180,
		},
	)
	if err != nil {
		return nil, err
	}
	data := new(Persister)
	mb, err := migrationbox.NewMigrationBox(migrations, c)
	if err != nil {
		return nil, err
	}
	data.conn = c
	data.mb = mb

	err = pop.CreateDB(data.conn)
	if err == nil {
		err = data.mb.CreateSchemaMigrations()
		if err != nil {
			return nil, err
		}
		err = data.mb.Up()
		if err != nil {
			return nil, err
		}
	}
	if migrate {
		err = data.mb.Up()
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (x *Persister) NewProfile(ctx context.Context, profileName string, data interface{}) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	profile.ID = uuid.Must(uuid.NewV4()).String()
	profile.ProfileName = profileName
	profile.ProfileData = NewJSONData(data)

	return profile, x.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			return HandleError(x.Connection(ctx).WithContext(ctx).Create(profile))
		},
	)
}

func (x *Persister) GetProfile(ctx context.Context, profileID string) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			return HandleError(x.Connection(ctx).WithContext(ctx).Select("profile.*").Find(profile, profileID))
		},
	)
}

func (x *Persister) FindProfile(ctx context.Context, profileName string) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			return HandleError(x.Connection(ctx).WithContext(ctx).Select("profile.*").Where("profile_name = ?", profileName).First(profile))
		},
	)
}

func (x *Persister) DeleteProfile(ctx context.Context, profileID string) error {
	return x.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			return HandleError(x.Connection(ctx).WithContext(ctx).Destroy(&Profile{ID: profileID}))
		},
	)
}

func (x *Persister) GetPKCE(ctx context.Context, state string) (*PKCE, error) {
	pkce := new(PKCE)
	pkce.persister = x
	return pkce, x.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			err := HandleError(x.Connection(ctx).Select("pkce.*").Where("state = ?", state).Where("created_at > ?", time.Now().Add(-5*time.Minute)).First(pkce))
			if err != nil {
				return err
			}
			return nil
		},
	)
}
