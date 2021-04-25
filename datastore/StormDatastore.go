package datastore

import (
	"github.com/ferocious-space/bolthold"
	jsoniter "github.com/json-iterator/go"
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
	return &BoltAccountStore{
		store: bh,
	}
}

func (x *BoltAccountStore) Create(data *AccountData) error {
	return x.store.Insert(data.Owner, data)
}

func (x *BoltAccountStore) SearchName(CharacterName string, Scopes []string) (data *AccountData, err error) {
	var result AccountData
	err = x.store.FindOne(&result, bolthold.Where("CharacterName").Eq(CharacterName).Index("CharacterName").And("Scopes").ContainsAll(bolthold.Slice(Scopes)...).Index("Scopes"))
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (x *BoltAccountStore) SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error) {
	var result AccountData
	err = x.store.FindOne(&result, bolthold.Where("CharacterId").Eq(CharacterID).Index("CharacterId").And("Scopes").ContainsAll(bolthold.Slice(Scopes)...).Index("Scopes"))
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (x *BoltAccountStore) Update(data *AccountData) error {
	return x.store.Update(data.Owner, data)
}

func (x *BoltAccountStore) Delete(data *AccountData) error {
	return x.store.Delete(data.Owner, data)
}
