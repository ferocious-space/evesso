package datastore

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"

	"github.com/pkg/errors"
	"go.etcd.io/bbolt"
	"google.golang.org/protobuf/encoding/protojson"
)

var ErrInvalidAccountData = errors.New("invalid account byteData")
var ErrNameNotFound = errors.New("CharacterName not found")
var ErrIDNotFound = errors.New("CharacterID not found")
var ErrAccountNotFound = errors.New("AccountData not found")
var ErrIndexIsEmpty = errors.New("index is empty")

func toByte(input interface{}) byteData {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, input); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
func fromByte(input []byte) (output interface{}) {
	buf := bytes.NewBuffer(input)
	if err := binary.Read(buf, binary.LittleEndian, output); err != nil {
		panic(err)
	}
	return output
}

func MatchScopes(x, y []string) bool {
	aLen := len(x)
	bLen := len(y)

	if aLen != bLen {
		return false
	}

	if aLen > 20 {
		return elementsMatchByMap(x, y)
	} else {
		return elementsMatchByLoop(x, y)
	}

}

func elementsMatchByLoop(listA, listB []string) bool {
	aLen := len(listA)
	bLen := len(listB)

	visited := make([]bool, bLen)

	for i := 0; i < aLen; i++ {
		found := false
		element := listA[i]
		for j := 0; j < bLen; j++ {
			if visited[j] {
				continue
			}
			if element == listB[j] {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func elementsMatchByMap(x, y []string) bool {
	// create a map of string -> int
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y] -= 1
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	if len(diff) == 0 {
		return true
	}
	return false
}

type byteData []byte

func MakeField(field interface{}) byteData {
	switch x := field.(type) {
	case string:
		return byteData(x)
	default:
		return toByte(field)
	}
}

func (x *byteData) Equal(y byteData) bool {
	return bytes.Equal(*x, y)
}

func (x *byteData) Append(field interface{}) byteData {
	return append(*x, MakeField(field)...)
}

func (x *byteData) Prepend(field interface{}) byteData {
	return append(MakeField(field), *x...)
}

type boltIndex struct {
	b *bbolt.Bucket
}

func newBoltIndex(b *bbolt.Bucket, name byteData) (*boltIndex, error) {
	bkt, err := b.CreateBucketIfNotExists(name.Prepend("index:"))
	if err != nil {
		return nil, err
	}
	return &boltIndex{b: bkt}, nil
}

func (x *boltIndex) Add(key, data byteData) error {
	current := make([]byteData, 0)
	bin := x.b.Get(key)
	if bin == nil {
		store, err := json.Marshal(append(current, data))
		if err != nil {
			return err
		}
		if err := x.b.Put(key, store); err != nil {
			return err
		}
		return nil
	}
	if err := json.Unmarshal(bin, &current); err != nil {
		return err
	}
	store, err := json.Marshal(append(current, data))
	if err != nil {
		return err
	}
	if err := x.b.Put(key, store); err != nil {
		return err
	}
	return nil
}

func (x *boltIndex) Remove(key, data byteData) error {
	current := make([]byteData, 0)
	bin := x.b.Get(key)
	if bin == nil {
		return ErrIndexIsEmpty
	}
	if err := json.Unmarshal(bin, &current); err != nil {
		return err
	}
	latest := make([]byteData, 0)
	for _, v := range current {
		if !reflect.DeepEqual(v, data) {
			latest = append(latest, v)
			break
		}
	}
	store, err := json.Marshal(append(current, data))
	if err != nil {
		return err
	}
	if err := x.b.Put(key, store); err != nil {
		return err
	}
	return nil
}

func (x *boltIndex) Get(key byteData) ([]byteData, error) {
	current := make([]byteData, 0)
	bin := x.b.Get(key)
	if bin == nil {
		return nil, ErrIndexIsEmpty
	}
	if err := json.Unmarshal(bin, &current); err != nil {
		return nil, err
	}
	return current, nil
}

type BoltAccountStore struct {
	db *bbolt.DB
}

func NewBoltAccountStore(db *bbolt.DB) *BoltAccountStore {
	return &BoltAccountStore{db: db}
}

func (b *BoltAccountStore) Create(data *AccountData) error {
	if !data.Valid() {
		return ErrInvalidAccountData
	}
	tx, err := b.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bkt, err := tx.CreateBucketIfNotExists(MakeField("AccountData"))
	if err != nil {
		return err
	}
	seq, err := bkt.NextSequence()
	if err != nil {
		return err
	}
	binSeq := MakeField(seq)

	bin, err := protojson.Marshal(data)
	if err != nil {
		return err
	}
	if err := bkt.Put(binSeq, bin); err != nil {
		return err
	}

	idxName, err := newBoltIndex(bkt, MakeField("CharacterName"))
	if err != nil {
		return err
	}
	if err := idxName.Add(MakeField(data.GetCharacterName()), binSeq); err != nil {
		return nil
	}

	idxID, err := newBoltIndex(bkt, MakeField("CharacterID"))
	if err != nil {
		return err
	}
	if err := idxID.Add(MakeField(data.CharacterId), binSeq); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *BoltAccountStore) SearchName(CharacterName string, Scopes []string) (data *AccountData, err error) {
	data = new(AccountData)
	tx, err := b.db.Begin(true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	bkt, err := tx.CreateBucketIfNotExists(MakeField("AccountData"))
	if err != nil {
		return nil, err
	}

	idxName, err := newBoltIndex(bkt, MakeField("CharacterName"))
	if err != nil {
		return nil, err
	}
	bin, err := idxName.Get(MakeField(CharacterName))
	if err != nil {
		return nil, err
	}
	for _, ptr := range bin {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, data); err != nil {
			continue
		}
		if MatchScopes(data.GetScopes(), Scopes) {
			return data, nil
		}
	}

	return nil, ErrNameNotFound
}

func (b *BoltAccountStore) SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error) {
	data = new(AccountData)
	tx, err := b.db.Begin(true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	bkt, err := tx.CreateBucketIfNotExists(MakeField("AccountData"))
	if err != nil {
		return nil, err
	}
	idxID, err := newBoltIndex(bkt, MakeField("CharacterID"))
	if err != nil {
		return nil, err
	}
	bin, err := idxID.Get(toByte(CharacterID))
	if err != nil {
		return nil, err
	}

	for _, ptr := range bin {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, data); err != nil {
			continue
		}
		if MatchScopes(data.GetScopes(), Scopes) {
			return data, nil
		}
	}

	return nil, ErrIDNotFound
}

func (b *BoltAccountStore) Update(data *AccountData) error {
	current := new(AccountData)
	tx, err := b.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	bkt, err := tx.CreateBucketIfNotExists(MakeField("AccountData"))
	if err != nil {
		return err
	}

	idxID, err := newBoltIndex(bkt, MakeField("CharacterID"))
	if err != nil {
		return nil
	}

	idxName, err := newBoltIndex(bkt, MakeField("CharacterName"))
	if err != nil {
		return nil
	}

	namePTR, err := idxName.Get(MakeField(data.GetCharacterName()))
	if err != nil {
		return err
	}

	idPTRs, err := idxID.Get(MakeField(data.GetCharacterId()))
	if err != nil {
		return err
	}

	for _, ptr := range namePTR {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, current); err != nil {
			continue
		}
		if MatchScopes(current.GetScopes(), data.Scopes) {

			bin, err := protojson.Marshal(data)
			if err != nil {
				return err
			}
			if err := bkt.Put(ptr, bin); err != nil {
				return err
			}
			return tx.Commit()
		}
	}

	for _, ptr := range idPTRs {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, current); err != nil {
			continue
		}
		if MatchScopes(current.GetScopes(), data.Scopes) {
			bin, err := protojson.Marshal(data)
			if err != nil {
				return err
			}
			if err := bkt.Put(ptr, bin); err != nil {
				return err
			}
			return tx.Commit()
		}
	}

	return ErrAccountNotFound
}

func (b *BoltAccountStore) Delete(data *AccountData) error {
	current := new(AccountData)
	tx, err := b.db.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	bkt, err := tx.CreateBucketIfNotExists(MakeField("AccountData"))
	if err != nil {
		return err
	}

	idxID, err := newBoltIndex(bkt, MakeField("CharacterID"))
	if err != nil {
		return nil
	}

	idxName, err := newBoltIndex(bkt, MakeField("CharacterName"))
	if err != nil {
		return nil
	}

	namePTR, err := idxName.Get(MakeField(data.GetCharacterName()))
	if err != nil {
		return err
	}

	idPTRs, err := idxID.Get(MakeField(data.GetCharacterId()))
	if err != nil {
		return err
	}

	for _, ptr := range namePTR {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, current); err != nil {
			continue
		}
		if MatchScopes(current.GetScopes(), data.Scopes) && data.RefreshToken == current.RefreshToken {

			if err := bkt.Delete(ptr); err != nil {
				return err
			}
			_ = idxName.Remove(MakeField(data.CharacterName), ptr)
			_ = idxID.Remove(MakeField(data.CharacterId), ptr)
			return tx.Commit()
		}
	}

	for _, ptr := range idPTRs {
		binData := bkt.Get(ptr)
		if err := protojson.Unmarshal(binData, current); err != nil {
			continue
		}
		if MatchScopes(current.GetScopes(), data.Scopes) {

			if err := bkt.Delete(ptr); err != nil {
				return err
			}
			_ = idxName.Remove(MakeField(data.CharacterName), ptr)
			_ = idxID.Remove(MakeField(data.CharacterId), ptr)
			return tx.Commit()
		}
	}
	return ErrAccountNotFound
}
