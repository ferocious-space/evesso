package datastore

import (
	"github.com/asdine/storm"
	"go.etcd.io/bbolt"
)

type StormAccountStore struct {
	db *storm.DB
}

func NewStormAccountStore(boltDB *bbolt.DB) AccountStore {
	s, err := storm.Open("", storm.UseDB(boltDB))
	if err != nil {
		panic(err)
	}
	err = s.Init(&AccountData{})
	if err != nil {
		panic(err)
	}
	return &StormAccountStore{db: s}
}

func (x *StormAccountStore) Create(data *AccountData) error {
	tx, err := x.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = tx.Save(data)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (x *StormAccountStore) SearchName(CharacterName string, Scopes []string) (data *AccountData, err error) {
	tx, err := x.db.Begin(false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var accounts []AccountData
	err = tx.Find("CharacterName", CharacterName, &accounts)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if MatchScopes(Scopes, a.Scopes) {
			return &a, nil
		}
	}
	return nil, ErrNotFound
}

func (x *StormAccountStore) SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error) {
	tx, err := x.db.Begin(false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var accounts []AccountData
	err = tx.Find("CharacterID", CharacterID, &accounts)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if MatchScopes(Scopes, a.Scopes) {
			return &a, nil
		}
	}
	return nil, ErrNotFound
}

func (x *StormAccountStore) Update(data *AccountData) error {
	tx, err := x.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = tx.Update(data)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (x *StormAccountStore) Delete(data *AccountData) error {
	tx, err := x.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = tx.DeleteStruct(data)
	if err != nil {
		return err
	}
	return tx.Commit()
}
