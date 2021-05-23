package datastore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/jackc/pgx/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"

	"github.com/ferocious-space/evesso/internal/embedfs"
)

const transactionSSOKey = "transactionSSOKey"

//go:embed migrations/*.sql
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
	sqlx       *sqlx.DB
	migrations *migrate.Migrate
	//mb   *pop.Migrator
}

func (x *Persister) BeginTX(ctx context.Context) (context.Context, error) {
	_, ok := ctx.Value(transactionSSOKey).(*sqlx.Tx)
	if ok {
		return ctx, ErrTranscationOpen
	}
	tx, err := x.sqlx.BeginTxx(
		ctx, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  false,
		},
	)
	return context.WithValue(ctx, transactionSSOKey, tx), err
}
func (x *Persister) Commit(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(*sqlx.Tx)
	if !ok || c == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.Commit()
}
func (x *Persister) Rollback(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(*sqlx.Tx)
	if !ok || c == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.Rollback()
}
func (x *Persister) Connection(ctx context.Context) (*sqlx.Tx, error) {
	if c, ok := ctx.Value(transactionSSOKey).(*sqlx.Tx); ok {
		return c, nil
	}
	resultCtx, err := x.BeginTX(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return x.Connection(resultCtx)
}

func (x *Persister) tx(ctx context.Context, f func(ctx context.Context, tx *sqlx.Tx) error) error {
	var err error
	isNested := true
	c, ok := ctx.Value(transactionSSOKey).(*sqlx.Tx)
	if !ok {
		isNested = false
		c, err = x.Connection(ctx)
		if err != nil {
			logr.FromContextOrDiscard(ctx).Error(err, "connection")
			return errors.WithStack(err)
		}
	}

	if err := f(context.WithValue(ctx, transactionSSOKey, c), c); err != nil {
		if !isNested {
			if err := c.Rollback(); err != nil {
				logr.FromContextOrDiscard(ctx).Error(err, "nested transaction")
				return errors.WithStack(err)
			}
		}
		logr.FromContextOrDiscard(ctx).Error(err, "transaction")
		return HandleError(err)
	}
	if !isNested {
		return errors.WithStack(c.Commit())
	}
	return nil
}

type migrationLogger struct {
	log     logr.Logger
	verbose bool
}

func newMigrationLogger(log logr.Logger, verbose bool) *migrationLogger {
	return &migrationLogger{log: log, verbose: verbose}
}

func (m *migrationLogger) Printf(format string, v ...interface{}) {
	m.log.Info(fmt.Sprintf(format, v))
}

func (m *migrationLogger) Verbose() bool {
	return m.verbose
}

func NewPersister(ctx context.Context, dsn string, migrateForce bool) (*Persister, error) {
	var err error

	driver, err := embedfs.New(migrations, "migrations")
	if err != nil {
		return nil, err
	}

	data := new(Persister)
	data.migrations, err = migrate.NewWithSourceInstance("embedFS", driver, dsn)
	if err != nil {
		return nil, err
	}
	data.sqlx, err = sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	err = data.migrations.Migrate(1)
	if err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return nil, err
		}
	}
	data.migrations.Log = newMigrationLogger(logr.FromContextOrDiscard(ctx), true)
	return data, nil
}

func (x *Persister) NewProfile(ctx context.Context, profileName string, data interface{}) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	profile.ID = uuid.Must(uuid.NewV4()).String()
	profile.ProfileName = profileName
	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()

	return profile, x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := `INSERT INTO profiles (id, profile_name, created_at, updated_at) values (:id,:profile_name, :created_at, :updated_at)`
			logr.FromContextOrDiscard(ctx).Info(q)
			_, err := tx.NamedExecContext(ctx, q, profile)
			if err != nil {
				return err
			}
			return nil
		},
	)
}

func (x *Persister) GetProfile(ctx context.Context, profileID string) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("SELECT id, profile_name, created_at, updated_at FROM profiles WHERE id = ?")
			logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
			return tx.QueryRowxContext(ctx, q, profileID).StructScan(profile)
		},
	)
}

func (x *Persister) FindProfile(ctx context.Context, profileName string) (*Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind(`SELECT id,profile_name,created_at,updated_at from profiles where profile_name = ?`)
			logr.FromContextOrDiscard(ctx).Info(q, "name", profileName)
			return tx.QueryRowxContext(ctx, q, profileName).StructScan(profile)
		},
	)
}

func (x *Persister) DeleteProfile(ctx context.Context, profileID string) error {
	return x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind(`DELETE FROM profiles where id = ?`)
			logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
			_, err := tx.ExecContext(ctx, q, profileID)
			return err
		},
	)
}

func (x *Persister) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string, scopes Scopes) (*Profile, *Character, error) {
	character := new(Character)
	err := x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			query := make(map[string]interface{})
			queryParams := make([]string, 0)
			dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s LIMIT 1`

			if characterID > 0 {
				query["character_id"] = characterID
				queryParams = append(queryParams, `character_id = :character_i`)
			}
			if len(characterName) > 0 {
				query["character_name"] = characterName
				queryParams = append(queryParams, `character_name = :character_name`)
			}
			if len(Owner) > 0 {
				query["owner"] = Owner
				queryParams = append(queryParams, `owner = :owner`)
			}
			query["active"] = true
			queryParams = append(queryParams, `active = :active`)

			q := fmt.Sprintf(dataQuery, strings.Join(queryParams, " AND "))
			logr.FromContextOrDiscard(ctx).Info(q)
			namedContext, err := tx.PrepareNamedContext(ctx, q)
			if err != nil {
				return err
			}
			return namedContext.QueryRowxContext(ctx, query).StructScan(character)
		},
	)
	if err != nil {
		return nil, nil, err
	}

	profile, err := x.GetProfile(ctx, character.ProfileReference)
	if err != nil {
		return nil, nil, err
	}
	return profile, character, nil
}

func (x *Persister) GetPKCE(ctx context.Context, state string) (*PKCE, error) {
	pkce := new(PKCE)
	pkce.persister = x
	return pkce, x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind("SELECT id, profile_ref, state, code_verifier, code_challange, code_challange_method, created_at from pkces where state = ? and created_at > ? limit 1")
			logr.FromContextOrDiscard(ctx).Info(q, "state", state)
			return tx.QueryRowxContext(ctx, q, state, time.Now().Add(-5*time.Minute)).StructScan(pkce)
		},
	)
}

func (x *Persister) CleanPKCE(ctx context.Context) error {
	return x.tx(
		ctx, func(ctx context.Context, tx *sqlx.Tx) error {
			q := tx.Rebind(`delete from pkces where created_at < ?`)
			logr.FromContextOrDiscard(ctx).Info(q)
			rows, err := tx.ExecContext(ctx, q, time.Now().Add(-(5*time.Minute + 1*time.Second)))
			if err != nil {
				return err
			}
			affected, err := rows.RowsAffected()
			if err != nil {
				return err
			}
			logr.FromContextOrDiscard(ctx).Info(q, "deleted", affected)
			return nil
		},
	)
}
