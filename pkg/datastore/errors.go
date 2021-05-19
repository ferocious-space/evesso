package datastore

import (
	"database/sql"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgconn"
	"github.com/lib/pq"
	"github.com/pkg/errors"
)

var (
	ErrUniqueViolation  = errors.New("Unable to insert or update resource because a resource with that value already exists")
	ErrConcurrentUpdate = errors.New("Unable to serialize access due to a concurrent update in another session")
	ErrNoRows           = errors.New("Unable to locate the resource")
)

func HandleError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return errors.WithStack(ErrNoRows)
	}

	switch e := errors.Cause(err).(type) {
	case interface{ SQLState() string }:
		switch e.SQLState() {
		case "23505": // "unique_violation"
			return errors.Wrap(ErrUniqueViolation, err.Error())
		case "40001": // "serialization_failure"
			return errors.Wrap(ErrConcurrentUpdate, err.Error())
		}
	case *pq.Error:
		switch e.Code {
		case "23505": // "unique_violation"
			return errors.Wrap(ErrUniqueViolation, e.Error())
		case "40001": // "serialization_failure"
			return errors.Wrap(ErrConcurrentUpdate, e.Error())
		}
	case *mysql.MySQLError:
		switch e.Number {
		case 1062:
			return errors.Wrap(ErrUniqueViolation, err.Error())
		}
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
