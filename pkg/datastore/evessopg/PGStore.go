package evessopg

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"hash/crc32"
	"reflect"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/golang-migrate/migrate/v4"
	pgxm "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lann/builder"

	"github.com/ferocious-space/evesso"
)

//go:embed migrations/*.sql
var migrations embed.FS

var _ evesso.DataStore = &PGStore{}

type PGStore struct {
	sync.Mutex
	schema     string
	pool       *pgxpool.Pool
	lock       *pgxpool.Conn
	migrations *migrate.Migrate
}

func (x *PGStore) Setup(ctx context.Context, dsn string) error {
	ds, err := NewPGStore(ctx, dsn)
	if err != nil {
		return err
	}
	x.schema = ds.schema
	x.pool = ds.pool
	x.lock = ds.lock
	x.migrations = ds.migrations
	return nil
}

func (x *PGStore) Query(ctx context.Context, queryer sq.Sqlizer, output interface{}) error {
	q := builder.Set(queryer, "PlaceholderFormat", sq.Dollar).(sq.Sqlizer)
	rsql, args, err := q.ToSql()
	if err != nil {
		return err
	}
	typ := reflect.TypeOf(output)
	switch typ {
	case nil:
		switch queryer.(type) {
		case sq.SelectBuilder:
			return fmt.Errorf("output cannot be nil")
		case sq.InsertBuilder, sq.DeleteBuilder, sq.UpdateBuilder:
			return x.Transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err = tx.Exec(ctx, rsql, args...)
				if err != nil {
					return err
				}
				return nil
			})
		default:
			return errors.New("unknown query")
		}
	default:
		switch typ.Kind() {
		case reflect.Ptr:
			switch typ.Elem().Kind() {
			case reflect.Slice, reflect.Array:
				return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
					return pgxscan.Select(ctx, tx, output, rsql, args...)
				})
			default:
				return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
					return pgxscan.Get(ctx, tx, output, rsql, args...)
				})
			}
		default:
			return fmt.Errorf("must be pointer not %T", output)
		}
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
	return pgx.BeginTxFunc(
		ctx, x.pool, pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadWrite,
		}, func(tx pgx.Tx) error {
			return f(ctx, tx)
		},
	)
}

func (x *PGStore) GLock(key1 interface{}) {
	x.Lock()
	defer x.Unlock()
	ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(1)*time.Minute)
	defer cancel()
	if x.lock == nil {
		acquire, err := x.pool.Acquire(context.Background())
		if err != nil {
			return
		}
		x.lock = acquire
	}
	err := x.lock.Ping(ctx)
	if err != nil {
		panic(err)
	}
	switch t := key1.(type) {
	case int64:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_lock($1)", key1); err != nil {
			panic(err)
		}
	case int32:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_lock($1)", key1); err != nil {
			panic(err)
		}
	case int:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_lock($1)", key1); err != nil {
			panic(err)
		}
	case string:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_lock($1)", crc32.ChecksumIEEE([]byte(t))); err != nil {
			panic(err)
		}
	case []byte:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_lock($1)", crc32.ChecksumIEEE(t)); err != nil {
			panic(err)
		}
	default:
		panic("unknown type")
	}
}

func (x *PGStore) GUnlock(key1 interface{}) {
	x.Lock()
	defer x.Unlock()
	ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(1)*time.Minute)
	defer cancel()
	err := x.lock.Ping(ctx)
	if err != nil {
		panic(err)
	}

	switch t := key1.(type) {
	case int64:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", key1); err != nil {
			panic(err)
		}
	case int32:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", key1); err != nil {
			panic(err)
		}
	case int:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", key1); err != nil {
			panic(err)
		}
	case string:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", crc32.ChecksumIEEE([]byte(t))); err != nil {
			panic(err)
		}
	case []byte:
		if _, err = x.lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", crc32.ChecksumIEEE(t)); err != nil {
			panic(err)
		}
	default:
		panic("unknown type")
	}
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

	driver, err := iofs.New(migrations, "migrations")
	if err != nil {
		return nil, err
	}

	data := new(PGStore)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	data.pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	instance, err := pgxm.WithInstance(
		stdlib.OpenDB(*config.ConnConfig),
		&pgxm.Config{
			MigrationsTable:  "sso_migrations",
			SchemaName:       "evesso",
			DatabaseName:     config.ConnConfig.Database,
			StatementTimeout: 1 * time.Minute,
		},
	)
	if err != nil {
		return nil, err
	}
	data.migrations, err = migrate.NewWithInstance("iofs", driver, "postgres", instance)
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

func (x *PGStore) NewProfile(ctx context.Context, profileName string, data interface{}) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	profile.ProfileName = profileName
	if data != nil {
		marshalled, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		profile.Data = marshalled
	}
	now := time.Now()
	profile.CreatedAt = now
	profile.UpdatedAt = now
	err := x.Query(ctx,
		sq.Insert("evesso.profiles").
			Columns("profile_name", "data", "created_at", "updated_at").
			Values(profile.ProfileName, profile.Data, profile.CreatedAt, profile.UpdatedAt).
			Suffix("RETURNING id"),
		profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) AllProfiles(ctx context.Context) ([]evesso.Profile, error) {
	result := make([]evesso.Profile, 0)
	var profiles []*Profile
	err := x.Query(ctx, sq.Select("*").From("evesso.profiles"), &profiles)
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
	err := x.Query(ctx, sq.Select("*").From("evesso.profiles").Where(sq.Eq{"id": profileID}), profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) FindProfile(ctx context.Context, profileName string) (evesso.Profile, error) {
	profile := new(Profile)
	profile.store = x
	err := x.Query(ctx, sq.Select("*").From("evesso.profiles").Where(sq.Eq{"profile_name": profileName}), profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (x *PGStore) DeleteProfile(ctx context.Context, profileID uuid.UUID) error {
	err := x.Query(ctx, sq.Delete("evesso.profiles").Where(sq.Eq{"id": profileID}), nil)
	if err != nil {
		return err
	}
	return nil
}

func (x *PGStore) FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string) (evesso.Profile, evesso.Character, error) {
	character := new(Character)
	character.store = x

	wh := sq.Select("*").From("evesso.characters")
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
	err := x.Query(ctx, wh.Where(wcl), character)
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
	err := x.Query(ctx, sq.Select("*").
		From("evesso.pkces").
		Where(
			sq.And{
				sq.Eq{"id": pkceID},
				sq.Gt{"created_at": time.Now().Add(-5 * time.Minute)},
			}),
		pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (x *PGStore) FindPKCE(ctx context.Context, state uuid.UUID) (evesso.PKCE, error) {
	pkce := new(PKCE)
	pkce.store = x
	err := x.Query(ctx,
		sq.Select("*").
			From("evesso.pkces").
			Where(
				sq.And{
					sq.Eq{"state": state},
					sq.Gt{"created_at": time.Now().Add(-5 * time.Minute)},
				}),
		pkce)
	if err != nil {
		return nil, err
	}
	return pkce, nil
}

func (x *PGStore) CleanPKCE(ctx context.Context) error {
	err := x.Query(ctx, sq.Delete("evesso.pkces").
		Where(
			sq.Lt{"created_at": time.Now().Add(-(5*time.Minute + 1*time.Second))},
		), nil)
	if err != nil {
		return err
	}
	return nil
}
