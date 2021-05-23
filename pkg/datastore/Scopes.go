package datastore

import (
	"database/sql/driver"
	"sort"

	"github.com/goccy/go-json"
	"github.com/pkg/errors"
)

type Scopes []string

func (s Scopes) Value() (driver.Value, error) {
	scp := s[:]
	sort.Strings(scp)
	return json.Marshal(scp)
}

func (s *Scopes) Scan(src interface{}) error {
	data, ok := src.([]byte)
	if !ok {
		return errors.Errorf("unable to unmarshal Scopes value: %v", src)
	}
	return json.Unmarshal(data, &s)
}
