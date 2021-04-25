package datastore

import (
	"crypto/sha256"
	"sort"
	"strings"

	"github.com/ferocious-space/bolthold"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"
)

type BoltAccountStore struct {
	store *bolthold.Store
}

func NewBoltAccountStore(boltDB *bbolt.DB) AccountStore {
	bh, err := bolthold.Wrap(
		boltDB, &bolthold.Options{
			Encoder: func(value interface{}) ([]byte, error) {
				return jsoniter.Marshal(value)
			},
			Decoder: func(data []byte, value interface{}) error {
				return jsoniter.Unmarshal(data, value)
			},
		},
	)
	if err != nil {
		panic(err)
	}
	err = bh.ReIndex(&AccountData{}, nil)
	if err != nil {
		return nil
	}
	return &BoltAccountStore{
		store: bh,
	}
}

func (x *BoltAccountStore) Create(data *AccountData) error {
	return x.store.Insert(sha256.New().Sum([]byte(strings.Join(data.Scopes, ", ")+data.Owner)), data)
}

func (x *BoltAccountStore) SearchName(CharacterName string, Scopes []string) (data *AccountData, err error) {
	var result AccountData
	scp := Scopes[:]
	sort.Strings(scp)
	err = x.store.FindOne(&result, bolthold.Where("CharacterName").Eq(CharacterName).Index("CharacterName").And("Scopes").ContainsAll(bolthold.Slice(scp)...))
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (x *BoltAccountStore) SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error) {
	var result AccountData
	scp := Scopes[:]
	sort.Strings(scp)
	err = x.store.FindOne(&result, bolthold.Where("CharacterId").Eq(CharacterID).Index("CharacterId").And("Scopes").ContainsAll(bolthold.Slice(scp)...))
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (x *BoltAccountStore) Update(data *AccountData) error {
	scp := data.Scopes[:]
	sort.Strings(scp)
	return x.store.UpdateMatching(
		data, bolthold.Where("Owner").Eq(data.Owner).Index("Owner").And("Scopes").ContainsAll(bolthold.Slice(scp)...), func(record interface{}) error {
			update, ok := record.(*AccountData)
			if !ok {
				return errors.Errorf("invalid record: %T", record)
			}
			*update = *data
			return nil
		},
	)
}

func (x *BoltAccountStore) Delete(data *AccountData) error {
	scp := data.Scopes[:]
	sort.Strings(scp)
	return x.store.DeleteMatching(data, bolthold.Where("Owner").Eq(data.Owner).Index("Owner").And("Scopes").ContainsAll(bolthold.Slice(scp)...))
}
