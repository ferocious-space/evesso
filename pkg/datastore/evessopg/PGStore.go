package evessopg

import (
	"context"
	"embed"
	"fmt"
	"hash/crc32"
	"reflect"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/golang-migrate/migrate/v4"
	pgxm "github.com/golang-migrate/migrate/v4/database/pgx"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/log/logrusadapter"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/lann/builder"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/pkg/datastore/embedfs"
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
	*x = *ds
	return nil
}

func (x *PGStore) Query(ctx context.Context, queryer sq.Sqlizer, output interface{}) error {
	q := builder.Set(queryer, "PlaceholderFormat", sq.Dollar).(sq.Sqlizer)
	rsql, args, err := q.ToSql()
	if err != nil {
		return err
	}
	logr.FromContextOrDiscard(ctx).Info(rsql, "args", args)
	return x.Connection(ctx, func(ctx context.Context, tx *pgxpool.Conn) error {
		typ := reflect.TypeOf(output)
		switch typ {
		case nil:
			switch queryer.(type) {
			case sq.SelectBuilder:
				return errors.Errorf("output cannot be nil")
			case sq.InsertBuilder, sq.DeleteBuilder, sq.UpdateBuilder:
				_, err := tx.Exec(ctx, rsql, args...)
				if err != nil {
					return err
				}
				return nil
			default:
				return errors.New("unknown query")
			}
		default:
			switch typ.Kind() {
			case reflect.Ptr:
				switch typ.Elem().Kind() {
				case reflect.Slice, reflect.Array:
					return pgxscan.Select(ctx, tx, output, rsql, args...)
				default:
					return pgxscan.Get(ctx, tx, output, rsql, args...)
				}
			default:
				return errors.Errorf("must be pointer not %T", output)
			}
		}
	})
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

	driver, err := embedfs.New(migrations, "migrations")
	if err != nil {
		return nil, err
	}

	data := new(PGStore)
	config, err := pgxpool.ParseConfig(dsn)
	config.ConnConfig.LogLevel = pgx.LogLevelTrace
	config.ConnConfig.Logger = logrusadapter.NewLogger(logrus.New())
	if err != nil {
		return nil, err
	}
	data.pool, err = pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	instance, err := pgxm.WithInstance(
		stdlib.OpenDB(*config.ConnConfig),
		&pgxm.Config{
			MigrationsTable:  "migrations",
			DatabaseName:     config.ConnConfig.Database,
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
	err := x.Query(ctx, InsertGenerate("evesso.profiles", profile).Suffix("RETURNING id"), profile)
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

//func (x *PGStore) FindCharacter(ctx context.Context, IDorName interface{}) (evesso.Profile, evesso.Character, error) {
//	character := new(Character)
//	character.store = x
//
//	wh := sq.Select("*").From("evesso.characters")
//	wcl := sq.And{}
//	switch data := IDorName.(type) {
//	case int32:
//		wcl = append(wcl, sq.Eq{"character_id": data})
//	case string:
//		wcl = append(wcl, sq.Eq{"character_name": data})
//	default:
//		return nil, nil, errors.Errorf("IDorName(%T) must be int32 or string", IDorName)
//	}
//	wcl = append(wcl, sq.Eq{"active": true})
//	err := x.Query(ctx, wh.Where(wcl), character)
//	if err != nil {
//		return nil, nil, err
//	}
//	profile, err := x.GetProfile(ctx, character.GetProfileID())
//	if err != nil {
//		return nil, nil, err
//	}
//	return profile, character, nil
//}

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
