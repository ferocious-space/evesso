package migrationbox

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/gobuffalo/pop/v5"
	"github.com/pkg/errors"
)

type MigrationBox struct {
	*pop.Migrator
	Dir embed.FS
}

func NewMigrationBox(Dir embed.FS, c *pop.Connection) (*pop.Migrator, error) {
	mb := &pop.Migrator{
		Connection: c,
		Migrations: map[string]pop.Migrations{
			"up":   {},
			"down": {},
		},
	}

	runner := func(b fs.File) func(pop.Migration, *pop.Connection) error {
		return func(migration pop.Migration, connection *pop.Connection) error {
			content, err := pop.MigrationContent(migration, connection, b, true)
			if err != nil {
				return err
			}
			if content == "" {
				return nil
			}
			err = connection.RawQuery(content).Exec()
			if err != nil {
				return errors.Wrapf(err, "error executing %s, sql: %s", migration.Path, content)
			}
			return nil
		}
	}

	err := fs.WalkDir(
		Dir, ".", func(p string, info fs.DirEntry, err error) error {
			if err != nil {
				return errors.WithStack(err)
			}
			if info.IsDir() {
				return nil
			}

			match, err := pop.ParseMigrationFilename(info.Name())
			if err != nil {
				if strings.HasPrefix(err.Error(), "unsupported dialect") {
					return nil
				}
				return errors.WithStack(err)
			}
			if match == nil {
				return nil
			}
			content, err := Dir.Open(p)
			if err != nil {
				return errors.WithStack(err)
			}
			mf := pop.Migration{
				Path:      p,
				Version:   match.Version,
				Name:      match.Name,
				Direction: match.Direction,
				Type:      match.Type,
				DBType:    match.DBType,
				Runner:    runner(content),
			}
			mb.Migrations[mf.Direction] = append(mb.Migrations[mf.Direction], mf)
			mod := sortIdent(mb.Migrations[mf.Direction])
			if mf.Direction == "down" {
				mod = sort.Reverse(mod)
			}
			sort.Sort(mod)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return mb, nil
}

func sortIdent(i sort.Interface) sort.Interface {
	return i
}
