package datastore

import (
	"sort"
)

type MemoryAccountStore struct {
	accounts []AccountData
}

func listsMatch(listone []string, listtwo []string) bool {
	if len(listone) != len(listtwo) {
		return false
	}
	sort.Strings(listone)
	sort.Strings(listtwo)
	for i := range listone {
		if listone[i] != listtwo[i] {
			return false
		}
	}
	return true
}

func NewMemoryAccountStore() *MemoryAccountStore {
	return &MemoryAccountStore{accounts: []AccountData{}}
}

func (m *MemoryAccountStore) Create(data *AccountData) error {
	for i := range m.accounts {
		if m.accounts[i].CharacterId == data.CharacterId && m.accounts[i].Owner == data.Owner {
			return ErrAlreadyExists
		}
	}
	m.accounts = append(m.accounts, *data)
	return nil
}

func (m *MemoryAccountStore) SearchName(CharacterName string, Scopes []string) (data *AccountData, err error) {
	for i := range m.accounts {
		if m.accounts[i].CharacterName == CharacterName && listsMatch(m.accounts[i].Scopes, Scopes) {
			return &m.accounts[i], nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryAccountStore) SearchID(CharacterID int32, Scopes []string) (data *AccountData, err error) {
	for i := range m.accounts {
		if m.accounts[i].CharacterId == CharacterID && listsMatch(m.accounts[i].Scopes, Scopes) {
			return &m.accounts[i], nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryAccountStore) Update(data *AccountData) error {
	for i := range m.accounts {
		if m.accounts[i].Owner == data.Owner && m.accounts[i].CharacterId == data.CharacterId {
			m.accounts[i] = *data
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryAccountStore) Delete(data *AccountData) error {
	for i := range m.accounts {
		if m.accounts[i].Owner == data.Owner && m.accounts[i].CharacterId == data.CharacterId {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
