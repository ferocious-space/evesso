package evessopg

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"reflect"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/pkg/errors"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/pkg/datastore/embedfs"
)

const transactionSSOKey = "transactionSSOKey"

//go:embed migrations/*.sql
var migrations embed.FS

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

func (x *PGStore) Exec(ctx context.Context, queryer sq.Sqlizer) error {
	switch q := queryer.(type) {
	case sq.UpdateBuilder:
		rsql, args, err := q.PlaceholderFormat(sq.Dollar).ToSql()
		if err != nil {
			return err
		}
		logr.FromContextOrDiscard(ctx).Info(rsql, "args", args)
		return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
			_, err := tx.Exec(ctx, rsql, args...)
			if err != nil {
				return err
			}
			return nil
		})
	case sq.DeleteBuilder:
		rsql, args, err := q.PlaceholderFormat(sq.Dollar).ToSql()
		if err != nil {
			return err
		}
		logr.FromContextOrDiscard(ctx).Info(rsql, "args", args)
		return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
			_, err := tx.Exec(ctx, rsql, args...)
			if err != nil {
				return err
			}
			return nil
		})
	default:
		return errors.New("unknown query")
	}
}

func (x *PGStore) Select(ctx context.Context, queryer sq.Sqlizer, output interface{}) error {
	switch q := queryer.(type) {
	case sq.SelectBuilder:
		rsql, args, err := q.PlaceholderFormat(sq.Dollar).ToSql()
		if err != nil {
			return err
		}
		logr.FromContextOrDiscard(ctx).Info(rsql, "args", args)
		return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
			switch reflect.TypeOf(output).Kind() {
			case reflect.Ptr:
				switch reflect.TypeOf(output).Elem().Kind() {
				case reflect.Slice, reflect.Array:
					return pgxscan.Select(ctx, tx, output, rsql, args...)
				default:
					return pgxscan.Get(ctx, tx, output, rsql, args...)
				}
			default:
				return errors.Errorf("must be pointer not %T", output)
			}
		})
	case sq.InsertBuilder:
		rsql, args, err := q.PlaceholderFormat(sq.Dollar).ToSql()
		if err != nil {
			return err
		}
		logr.FromContextOrDiscard(ctx).Info(rsql, "args", args)
		return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
			return pgxscan.Get(ctx, tx, output, rsql, args...)
		})
	default:
		return errors.New("unknown query")
	}
}

func (x *PGStore) Connection(ctx context.Context, f func(ctx context.Context, tx *pgxpool.Conn) error) error {
	tx, err := x.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer tx.Release()
	return f(ctx, tx)
}

func (x *PGStore) Transaction(ctx context.Context, f func(ctx context.Context, tx pgx.Tx) error) error {
	return x.pool.BeginTxFunc(
		ctx, pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadWrite,
		}, func(tx pgx.Tx) error {
			return f(ctx, tx)
		},
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
	m.log.Info(fmt.Sprintf(format, v...))
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

	schema := ""
	if config.ConnConfig.User == "postgres" {
		schema = "public"
		config.ConnConfig.RuntimeParams["search_path"] = "evesso, public"
	} else {
		schema = "evesso"
		config.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("evesso, %s, public", config.ConnConfig.User)
	}

	pool, err := pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	data.pool = pool

	if err := pool.AcquireFunc(
		ctx, func(conn *pgxpool.Conn) error {
			if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS evesso AUTHORIZATION %s", config.ConnConfig.User)); err != nil {
				return err
			}
			return nil
		},
	); err != nil {
		return nil, err
	}
	data.pool = pool

	sqlDB, err := sql.Open("pgx", stdlib.RegisterConnConfig(config.ConnConfig.Copy()))
	if err != nil {
		return nil, err
	}

	instance, err := postgres.WithInstance(
		sqlDB, &postgres.Config{
			MigrationsTable:  "schema_migrations",
			DatabaseName:     config.ConnConfig.Database,
			SchemaName:       schema,
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
	data.migrations.Log = newMigrationLogger(logr.FromContextOrDiscard(ctx), true)
	err = data.migrations.Up()
	if err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return nil, err
		}
	}

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
	err := x.Select(ctx,
		sq.Insert("profiles").
			Columns("profile_name", "created_at", "updated_at").
			Values(profile.ProfileName, profile.CreatedAt, profile.UpdatedAt).
			Suffix("returning id"),
		profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) AllProfiles(ctx context.Context) ([]evesso.Profile, error) {
	result := make([]evesso.Profile, 0)
	var profiles []*Profile
	err := x.Select(ctx, sq.Select("*").From("profiles"), &profiles)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		p.store = x
		result = append(result, p)
	}
	return result, nil
}

func (x *PGStore) GetProfile(ctx context.Context, profileID uuid.UUID) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	err := x.Select(ctx, sq.Select("*").From("profiles").Where(sq.Eq{"id": profileID}), profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) FindProfile(ctx context.Context, profileName string) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	err := x.Select(ctx, sq.Select("*").From("profiles").Where(sq.Eq{"profile_name": profileName}), profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) DeleteProfile(ctx context.Context, profileID uuid.UUID) error {
	err := x.Exec(ctx, sq.Delete("profiles").Where("id = ?", profileID))
	if err != nil {
		return err
	}
	return nil
}

func (x *PGStore) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string) (evesso.Profile, evesso.Character, error) {
	character := new(Character)
	character.store = x

	wh := sq.Select("*").From("characters")
	wcl := sq.And{}
	if characterID > 0 {
		wcl = append(wcl, sq.Eq{"character_id": characterID})
	}
	if len(characterName) > 0 {
		wcl = append(wcl, sq.Eq{"character_name": characterName})
	}
	if len(Owner) > 0 {
		wcl = append(wcl, sq.Eq{"owner": Owner})
	}
	wcl = append(wcl, sq.Eq{"active": true})
	err := x.Select(ctx, wh.Where(wcl), character)
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
	pkce.store = x
	err := x.Select(ctx, sq.Select("*").From("pkces").Where("id = ? AND created_at > ?", pkceID, time.Now().Add(-5*time.Minute)), pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (x *PGStore) FindPKCE(ctx context.Context, state uuid.UUID) (evesso.PKCE, error) {
	pkce := new(PKCE)
	pkce.store = x
	err := x.Select(ctx, sq.Select("*").From("pkces").Where("state = ? AND created_at > ?", state, time.Now().Add(-5*time.Minute)), pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (x *PGStore) CleanPKCE(ctx context.Context) error {
	err := x.Exec(ctx, sq.Delete("pkces").Where("created_at < ?", time.Now().Add(-(5*time.Minute+1*time.Second))))
	if err != nil {
		return err
	}
	return nil
}
