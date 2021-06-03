package postgres

import (
	"database/sql/driver"
	"sort"

	"github.com/goccy/go-json"
	"github.com/pkg/errors"
)

type Scope []string

func MakeScopes(in []string) *Scope {
	scope := Scope(in)
	return &scope
}

func (s Scope) Get() []string {
	return s
}

func (s Scope) Value() (driver.Value, error) {
	scp := s[:]
	sort.Strings(scp)
	return json.Marshal(scp)
}

func (s *Scope) Scan(src interface{}) error {
	data, ok := src.([]byte)
	if !ok {
		return errors.Errorf("unable to unmarshal Scope value: %v", src)
	}
	return json.Unmarshal(data, &s)
}
