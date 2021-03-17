package datastore

import (
	"sort"

	"github.com/pkg/errors"
)

var ErrNotFound = errors.New("token not found.")
var ErrAlreadyExists = errors.New("account already exists.")
var ErrOwner = errors.New("Owner dont match.")
var ErrCharacterID = errors.New("characterID dont match.")
var ErrCharacterName = errors.New("characterName dont match.")
var ErrScope = errors.New("scopes dont match.")

type AccountData struct {
	CharacterName string
	CharacterID   int64
	Owner         string
	RefreshToken  string
	Scopes        []string
}

type AccountStore interface {
	Create(characterName string, characterID int64, owner string, refreshToken string, scopes []string) error
	SearchName(CharacterName string, Scopes []string) (data *AccountData, err error)
	SearchID(CharacterID int64, Scopes []string) (data *AccountData, err error)
	SearchOwner(Owner string, Scopes []string) (data *AccountData, err error)
	Update(data *AccountData) error
	Delete(data *AccountData) error
}

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

func (m *MemoryAccountStore) Create(characterName string, characterID int64, owner string, refreshToken string, scopes []string) error {
	for i := range m.accounts {
		if m.accounts[i].CharacterID == characterID && m.accounts[i].Owner == owner {
			return ErrAlreadyExists
		}
	}
	m.accounts = append(m.accounts, AccountData{
		CharacterName: characterName,
		CharacterID:   characterID,
		Owner:         owner,
		RefreshToken:  refreshToken,
		Scopes:        scopes,
	})
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

func (m *MemoryAccountStore) SearchID(CharacterID int64, Scopes []string) (data *AccountData, err error) {
	for i := range m.accounts {
		if m.accounts[i].CharacterID == CharacterID && listsMatch(m.accounts[i].Scopes, Scopes) {
			return &m.accounts[i], nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryAccountStore) SearchOwner(Owner string, Scopes []string) (data *AccountData, err error) {
	for i := range m.accounts {
		if m.accounts[i].Owner == Owner && listsMatch(m.accounts[i].Scopes, Scopes) {
			return &m.accounts[i], nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryAccountStore) Update(data *AccountData) error {
	for i := range m.accounts {
		if m.accounts[i].Owner == data.Owner && m.accounts[i].CharacterID == data.CharacterID {
			m.accounts[i] = *data
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryAccountStore) Delete(data *AccountData) error {
	for i := range m.accounts {
		if m.accounts[i].Owner == data.Owner && m.accounts[i].CharacterID == data.CharacterID {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
