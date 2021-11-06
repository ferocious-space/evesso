package evessopg

import (
	"database/sql"
	"strings"

	//"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	//"github.com/lib/pq"
	"github.com/pkg/errors"
)

var (
	ErrUniqueViolation   = errors.New("Unable to insert or update resource because a resource with that value already exists")
	ErrConcurrentUpdate  = errors.New("Unable to serialize access due to a concurrent update in another session")
	ErrNoRows            = errors.New("Unable to locate the resource")
	ErrTokenScope        = errors.New("scope is missing")
	ErrTokenName         = errors.New("name is missing")
	ErrTokenOwner        = errors.New("owner is missing")
	ErrTokenID           = errors.New("id is missing")
	ErrTranscationOpen   = errors.New("Transaction already exist in this context")
	ErrNoTranscationOpen = errors.New("no Transaction in this context")
)

func HandleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.WithStack(ErrNoRows)
	}

	if errors.Is(err, sql.ErrNoRows) {
		return errors.WithStack(ErrNoRows)
	}
	switch e := errors.Cause(err).(type) {
	case *pgconn.PgError:
		switch e.Code {
		case "23505": // "unique_violation"
			return errors.Wrap(ErrUniqueViolation, e.Error())
		case "40001": // "serialization_failure"
			return errors.Wrap(ErrConcurrentUpdate, e.Error())
		}
	}
	// Try other detections, for example for SQLite (we don't want to enforce CGO here!)
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errors.Wrap(ErrUniqueViolation, err.Error())
	}

	return errors.WithStack(err)
}
