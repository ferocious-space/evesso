package postgres

import (
	"github.com/jackc/pgtype"
)

func MakeScopes(in []string) pgtype.TextArray {
	ta := pgtype.TextArray{}
	if err := ta.Set(in); err != nil {
		return pgtype.TextArray{}
	}
	return ta
}
