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
	"github.com/ferocious-space/evesso/pkg/datastore/embedfs"
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

//Connection have to call Release() after usage !
func (x *PGStore) Connection(ctx context.Context) (*pgxpool.Conn, error) {
	tx, err := x.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return tx, err
}

func (x *PGStore) transaction(ctx context.Context, f func(ctx context.Context, tx pgx.Tx) error) error {
	return HandleError(
		x.pool.BeginTxFunc(
			ctx, pgx.TxOptions{
				IsoLevel:   pgx.RepeatableRead,
				AccessMode: pgx.ReadWrite,
			}, func(tx pgx.Tx) error {
				return f(ctx, tx)
			},
		),
	)
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
	//if path, ok := config.ConnConfig.RuntimeParams["search_path"]; !ok {
	//	config.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("evesso, %s, public", config.ConnConfig.User)
	//	data.schema = "evesso"
	//} else {
	//	data.schema = path
	//}
	pool, err := pgxpool.ConnectConfig(ctx, config)
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
			SchemaName:       "evesso",
			StatementTimeout: 1 * time.Minute,
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

func (x *PGStore) NewProfile(ctx context.Context, profileName string) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	if err := profile.ProfileName.Set(profileName); err != nil {
		return nil, err
	}
	if err := profile.CreatedAt.Set(time.Now()); err != nil {
		return nil, err
	}
	if err := profile.UpdatedAt.Set(time.Now()); err != nil {
		return nil, err
	}
	return profile, x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `INSERT INTO profiles (profile_name, created_at, updated_at) values ($1 , $2 , $3) returning id`
			logr.FromContextOrDiscard(ctx).Info(q)
			err := tx.QueryRow(ctx, q, profile.ProfileName, profile.CreatedAt, profile.UpdatedAt).Scan(&profile.ID)
			if err != nil {
				return err
			}
			return nil
		},
	)
}

func (x *PGStore) GetProfile(ctx context.Context, profileID uuid.UUID) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	tx, err := x.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	q := "SELECT id,profile_name,created_at,updated_at FROM profiles WHERE id = $1"
	logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
	return profile, HandleError(tx.QueryRow(ctx, q, profileID).Scan(&profile.ID, &profile.ProfileName, &profile.CreatedAt, &profile.UpdatedAt))

}

func (x *PGStore) FindProfile(ctx context.Context, profileName string) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	tx, err := x.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()

	q := `SELECT id,profile_name,created_at,updated_at FROM profiles where profile_name = $1`
	logr.FromContextOrDiscard(ctx).Info(q, "name", profileName)
	return profile, HandleError(tx.QueryRow(ctx, q, profileName).Scan(&profile.ID, &profile.ProfileName, &profile.CreatedAt, &profile.UpdatedAt))

}

func (x *PGStore) DeleteProfile(ctx context.Context, profileID uuid.UUID) error {
	return x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `DELETE FROM profiles where id = $1`
			logr.FromContextOrDiscard(ctx).Info(q, "id", profileID)
			_, err := tx.Exec(ctx, q, profileID)
			if err != nil {
				return err
			}
			return nil
		},
	)
}

func (x *PGStore) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string) (evesso.Profile, evesso.Character, error) {
	character := new(Character)
	tx, err := x.Connection(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Release()

	dataQuery := `SELECT id, profile_ref, character_id, character_name, owner, refresh_token, scopes, active, created_at, updated_at FROM characters WHERE %s`
	whereParams := []string{}
	queryParams := []interface{}{}
	counter := 0
	if characterID > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("character_id = $%d", counter))
		queryParams = append(queryParams, characterID)
	}
	if len(characterName) > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("character_name = $%d", counter))
		queryParams = append(queryParams, characterName)
	}
	if len(Owner) > 0 {
		counter++
		whereParams = append(whereParams, fmt.Sprintf("owner = $%d", counter))
		queryParams = append(queryParams, Owner)
	}
	counter++
	whereParams = append(whereParams, fmt.Sprintf("active = $%d", counter))
	queryParams = append(queryParams, true)

	q := fmt.Sprintf(dataQuery, strings.Join(whereParams, " AND "))
	logr.FromContextOrDiscard(ctx).Info(q)
	err = tx.QueryRow(ctx, q, queryParams...).Scan(
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

	if err != nil {
		return nil, nil, err
	}

	profile, err := x.GetProfile(ctx, character.GetProfileID())
	if err != nil {
		return nil, nil, err
	}
	return profile, character, nil
}

func (x *PGStore) GetPKCE(ctx context.Context, pkceID uuid.UUID) (evesso.PKCE, error) {
	pkce := new(PKCE)
	tx, err := x.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	return pkce, tx.QueryRow(
		ctx,
		"SELECT id, profile_ref, state, code_verifier, code_challange, code_challange_method, scopes, created_at FROM pkces WHERE id = $1 AND created_at > $2",
		pkceID,
		time.Now().Add(-5*time.Minute),
	).Scan(
		&pkce.ID,
		&pkce.ProfileReference,
		&pkce.State,
		&pkce.CodeVerifier,
		&pkce.CodeChallange,
		&pkce.CodeChallangeMethod,
		&pkce.Scopes,
		&pkce.CreatedAt,
	)
}

func (x *PGStore) FindPKCE(ctx context.Context, state uuid.UUID) (evesso.PKCE, error) {
	pkce := new(PKCE)
	pkce.store = x
	tx, err := x.Connection(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Release()

	q := "SELECT id, profile_ref, state, code_verifier, code_challange, code_challange_method, scopes, created_at from pkces where state = $1 and created_at > $2"
	logr.FromContextOrDiscard(ctx).Info(q, "state", state)
	return pkce, HandleError(
		tx.QueryRow(ctx, q, state, time.Now().Add(-5*time.Minute)).Scan(
			&pkce.ID,
			&pkce.ProfileReference,
			&pkce.State,
			&pkce.CodeVerifier,
			&pkce.CodeChallange,
			&pkce.CodeChallangeMethod,
			&pkce.Scopes,
			&pkce.CreatedAt,
		),
	)

}

func (x *PGStore) CleanPKCE(ctx context.Context) error {
	return x.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := `delete from pkces where created_at < $1`
			logr.FromContextOrDiscard(ctx).Info(q)
			_, err := tx.Exec(ctx, q, time.Now().Add(-(5*time.Minute + 1*time.Second)))
			if err != nil {
				return err
			}
			return nil
		},
	)
}
