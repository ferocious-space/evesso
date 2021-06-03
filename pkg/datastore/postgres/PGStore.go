package postgres

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
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/pgx/v4/stdlib"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/pkg/errors"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/internal/embedfs"
)

const transactionSSOKey = "transactionSSOKey"

//go:embed migrations/*.sql
var migrations embed.FS

//type Transactional interface {
//	BeginTX(ctx context.Context) (context.Context, error)
//	Commit(ctx context.Context) error
//	Rollback(ctx context.Context) error
//}
//
//func MaybeBeginTx(ctx context.Context, storage interface{}) (context.Context, error) {
//	switch store := storage.(type) {
//	case Transactional:
//		return store.BeginTX(ctx)
//	default:
//		return ctx, nil
//	}
//}
//
//func MaybeCommitTx(ctx context.Context, storage interface{}) error {
//	switch store := storage.(type) {
//	case Transactional:
//		return store.Commit(ctx)
//	default:
//		return nil
//	}
//}
//
//func MaybeRollackTx(ctx context.Context, storage interface{}) error {
//	switch store := storage.(type) {
//	case Transactional:
//		return store.Rollback(ctx)
//	default:
//		return nil
//	}
//}

var _ evesso.DataStore = &PGStore{}

type PGStore struct {
	schema     string
	pool       *pgxpool.Pool
	migrations *migrate.Migrate
}

func (x *PGStore) Setup(ctx context.Context, dsn string) error {
	ds, err := NewPGStore(ctx, dsn)
	if err != nil {
		return err
	}
	*x = *ds
	return nil
}

func (x *PGStore) BeginTX(ctx context.Context) (context.Context, error) {
	_, ok := ctx.Value(transactionSSOKey).(pgx.Tx)
	if ok {
		return ctx, ErrTranscationOpen
	}
	tx, err := x.pool.BeginTx(
		ctx, pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadWrite,
		},
	)
	return context.WithValue(ctx, transactionSSOKey, tx), err
}
func (x *PGStore) Commit(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(pgx.Tx)
	if !ok || c == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.Commit(ctx)
}
func (x *PGStore) Rollback(ctx context.Context) error {
	c, ok := ctx.Value(transactionSSOKey).(pgx.Tx)
	if !ok || c == nil {
		return errors.WithStack(ErrNoTranscationOpen)
	}

	return c.Rollback(ctx)
}

func (x *PGStore) Connection(ctx context.Context) (pgx.Tx, error) {
	if c, ok := ctx.Value(transactionSSOKey).(pgx.Tx); ok {
		return c, nil
	}
	resultCtx, err := x.BeginTX(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return x.Connection(resultCtx)
}

func (x *PGStore) transaction(ctx context.Context, f func(ctx context.Context, tx pgx.Tx) error) error {
	var err error
	isNested := true
	c, ok := ctx.Value(transactionSSOKey).(pgx.Tx)
	if !ok {
		isNested = false
		c, err = x.Connection(ctx)
		if err != nil {
			logr.FromContextOrDiscard(ctx).Error(err, "connection")
			return errors.WithStack(err)
		}
	}
	tctx := context.WithValue(ctx, transactionSSOKey, c)
	if err := f(tctx, c); err != nil {
		if !isNested {
			if err := c.Rollback(ctx); err != nil {
				logr.FromContextOrDiscard(ctx).Error(err, "nested transaction")
				return errors.WithStack(err)
			}
		}
		logr.FromContextOrDiscard(ctx).Error(err, "transaction")
		return HandleError(err)
	}
	if !isNested {
		return errors.WithStack(c.Commit(ctx))
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

func NewPGStore(ctx context.Context, dsn string) (*PGStore, error) {
	var err error

	driver, err := embedfs.New(migrations, "migrations")
	if err != nil {
		return nil, err
	}

	data := new(PGStore)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 50
	config.MinConns = 5
	config.HealthCheckPeriod = 30 * time.Second
	config.MaxConnIdleTime = 1 * time.Minute
	config.MaxConnLifetime = 5 * time.Minute

	if path, ok := config.ConnConfig.RuntimeParams["search_path"]; !ok {
		config.ConnConfig.RuntimeParams["search_path"] = "evesso"
		data.schema = "evesso"
	} else {
		data.schema = path
	}

	pool, err := pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	err = pool.AcquireFunc(
		ctx, func(conn *pgxpool.Conn) error {
			_, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s", data.schema, config.ConnConfig.User))
			if err != nil {
				return err
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	data.pool = pool

	sqlDB, err := sql.Open("pgx", stdlib.RegisterConnConfig(config.ConnConfig))
	if err != nil {
		return nil, err
	}

	instance, err := postgres.WithInstance(
		sqlDB, &postgres.Config{
			MigrationsTable:  "schema_migrations",
			DatabaseName:     config.ConnConfig.Database,
			SchemaName:       data.schema,
			StatementTimeout: 0,
		},
	)
	if err != nil {
		return nil, err
	}

	data.migrations, err = migrate.NewWithInstance("embedFS", driver, "postgres", instance)
	if err != nil {
		return nil, err
	}

	err = data.migrations.Up()
	if err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return nil, err
		}
	}
	data.migrations.Log = newMigrationLogger(logr.FromContextOrDiscard(ctx), true)
	return data, nil
}

func (x *PGStore) NewProfile(ctx context.Context, profileName evesso.ProfileName) (evesso.Profile, error) {
	profile := new(Profile)
	profile.persister = x
	profile.ID = evesso.ProfileID(uuid.Must(uuid.NewV4()).String())
	profile.ProfileName = profileName
	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()

	return profile, x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `INSERT INTO profiles (id, profile_name, created_at, updated_at) values ($1 , $2 , $3 , $4)`
			logr.FromContextOrDiscard(ctx).Info(q)
			_, err := tx.Exec(ctx, q, profile.ID, profile.ProfileName, profile.CreatedAt, profile.UpdatedAt)
			if err != nil {
				return err
			}
			//_, err := tx.NamedExecContext(ctx, q, profile)
			//if err != nil {
			//	return err
			//}
			return nil
		},
	)
}

func (x *PGStore) GetProfile(ctx context.Context, profileID evesso.ProfileID) (evesso.Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "SELECT id,profile_name,created_at,updated_at FROM profiles WHERE id = $1"
			logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
			return tx.QueryRow(ctx, q, profileID).Scan(&profile.ID, &profile.ProfileName, &profile.CreatedAt, &profile.UpdatedAt)
			//q := tx.Rebind("SELECT id, profile_name, created_at, updated_at FROM profiles WHERE id = ?")
			//return tx.QueryRowxContext(ctx, q, profileID).StructScan(profile)
		},
	)
}

func (x *PGStore) FindProfile(ctx context.Context, profileName evesso.ProfileName) (evesso.Profile, error) {
	profile := new(Profile)
	profile.persister = x
	return profile, x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `SELECT id,profile_name,created_at,updated_at FROM profiles where profile_name = $1`
			//q := tx.Rebind(`SELECT id,profile_name,created_at,updated_at from profiles where profile_name = ?`)
			logr.FromContextOrDiscard(ctx).Info(q, "name", profileName)
			return tx.QueryRow(ctx, q, profileName).Scan(&profile.ID, &profile.ProfileName, &profile.CreatedAt, &profile.UpdatedAt)
			//return tx.QueryRowxContext(ctx, q, profileName).StructScan(profile)
		},
	)
}

func (x *PGStore) DeleteProfile(ctx context.Context, profileID evesso.ProfileID) error {
	return x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `DELETE FROM profiles where id = $1`
			logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
			_, err := tx.Exec(ctx, q, profileID)
			if err != nil {
				return err
			}
			return nil
			//q := tx.Rebind(`DELETE FROM profiles where id = ?`)
			//_, err := tx.ExecContext(ctx, q, profileID)
			//return err
		},
	)
}

func (x *PGStore) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string) (evesso.Profile, evesso.Character, error) {
	character := new(Character)
	err := x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			//query := make(map[string]interface{})
			//queryParams := make([]string, 0)
			dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s`
			whereParams := []string{}
			queryParams := []interface{}{}
			counter := 0
			if characterID > 0 {
				counter++
				whereParams = append(whereParams, fmt.Sprintf("character_id = $%d", counter))
				queryParams = append(queryParams, characterID)
				//query["character_id"] = characterID
				//queryParams = append(queryParams, `character_id = :character_i`)
			}
			if len(characterName) > 0 {
				counter++
				whereParams = append(whereParams, fmt.Sprintf("character_name = $%d", counter))
				queryParams = append(queryParams, characterName)
				//query["character_name"] = characterName
				//queryParams = append(queryParams, `character_name = :character_name`)
			}
			if len(Owner) > 0 {
				counter++
				whereParams = append(whereParams, fmt.Sprintf("owner = $%d", counter))
				queryParams = append(queryParams, Owner)
				//query["owner"] = Owner
				//queryParams = append(queryParams, `owner = :owner`)
			}
			counter++
			whereParams = append(whereParams, fmt.Sprintf("active = $%d", counter))
			queryParams = append(queryParams, true)
			//query["active"] = true
			//queryParams = append(queryParams, `active = :active`)

			q := fmt.Sprintf(dataQuery, strings.Join(whereParams, " AND "))
			logr.FromContextOrDiscard(ctx).Info(q)
			return tx.QueryRow(ctx, q, queryParams...).Scan(
				&character.ID,
				&character.ProfileReference,
				&character.CharacterName,
				&character.Owner,
				&character.RefreshToken,
				&character.Scopes,
				&character.Active,
				&character.CreatedAt,
				&character.UpdatedAt,
			)
			//namedContext, err := tx.PrepareNamedContext(ctx, q)
			//if err != nil {
			//	return err
			//}
			//return namedContext.QueryRowxContext(ctx, query).StructScan(character)
		},
	)
	if err != nil {
		return nil, nil, err
	}

	profile, err := x.GetProfile(ctx, character.GetProfileID())
	if err != nil {
		return nil, nil, err
	}
	return profile, character, nil
}

func (x *PGStore) GetPKCE(ctx context.Context, state string) (evesso.PKCE, error) {
	pkce := new(PKCE)
	pkce.persister = x
	return pkce, x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "SELECT id, profile_ref, state, code_verifier, code_challange, code_challange_method, created_at from pkces where state = $1 and created_at > $2"
			logr.FromContextOrDiscard(ctx).Info(q, "state", state)
			return tx.QueryRow(ctx, q, state, time.Now().Add(-5*time.Minute)).Scan(
				&pkce.ID,
				&pkce.ProfileReference,
				&pkce.State,
				&pkce.CodeVerifier,
				&pkce.CodeChallange,
				&pkce.CodeChallangeMethod,
				&pkce.CreatedAt,
			)
			//q := tx.Rebind("SELECT id, profile_ref, state, code_verifier, code_challange, code_challange_method, created_at from pkces where state = ? and created_at > ?")

			//return tx.QueryRowxContext(ctx, q, state, time.Now().Add(-5*time.Minute)).StructScan(pkce)
		},
	)
}

func (x *PGStore) CleanPKCE(ctx context.Context) error {
	return x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `delete from pkces where created_at < $1`
			//q := tx.Rebind(`delete from pkces where created_at < ?`)
			logr.FromContextOrDiscard(ctx).Info(q)
			_, err := tx.Exec(ctx, q, time.Now().Add(-(5*time.Minute + 1*time.Second)))
			if err != nil {
				return err
			}
			//rows, err := tx.ExecContext(ctx, q, time.Now().Add(-(5*time.Minute + 1*time.Second)))
			//if err != nil {
			//	return err
			//}
			//affected, err := rows.RowsAffected()
			//if err != nil {
			//	return err
			//}
			//logr.FromContextOrDiscard(ctx).Info(q, "deleted", affected)
			return nil
		},
	)
}
