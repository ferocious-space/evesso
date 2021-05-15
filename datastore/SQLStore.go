package datastore

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	err := db.AutoMigrate(&Profile{}, &Character{}, &PKCE{})
	if err != nil {
		return nil
	}
	return &Store{db: db}
}

func (s *Store) FindProfile(profileID uuid.UUID, profileName string) (*Profile, error) {
	profile := new(Profile)
	profile.store = s
	query := make(map[string]interface{}, 0)
	if profileID != uuid.Nil {
		query["id"] = profileID
	}
	if profileName != "" {
		query["profile_name"] = profileName
	}
	if len(query) == 0 {
		return nil, ErrNoQuery
	}
	rs := s.db.Where(query).First(profile)
	return profile, rs.Error
}

func (s *Store) DeleteCharacter(profileID uuid.UUID, profileName string, character *Character) error {
	profile, err := s.FindProfile(profileID, profileName)
	if err != nil {
		return err
	}
	searchCharacter, err := s.FindCharacter(profileID, character.CharacterID, character.CharacterName, character.Owner, character.Scopes...)
	if err != nil {
		return err
	}
	return s.db.Transaction(
		func(tx *gorm.DB) error {
			err := s.db.Model(profile).Association("Characters").Delete(searchCharacter)
			if err != nil {
				return err
			}
			return s.db.Delete(searchCharacter).Error
		},
	)
}

func (s *Store) CreateCharacter(profileID uuid.UUID, profileName string, character *Character) error {
	profile, err := s.FindProfile(profileID, profileName)
	if err != nil {
		return err
	}
	return s.db.Transaction(
		func(tx *gorm.DB) error {
			character.ProfileID = profile.ID
			rs := tx.Clauses(
				clause.OnConflict{
					UpdateAll: true,
				},
			).Create(character)
			return rs.Error
		},
	)
}

func (s *Store) DeleteProfile(profile *Profile) error {
	tx := s.db.Delete(profile)
	return tx.Error
}

func (s *Store) CreateProfile(profile *Profile) error {
	result := s.db.Create(profile)
	profile.store = s
	return result.Error
}

func (s *Store) FindCharacter(profileID uuid.UUID, characterID int32, characterName string, Owner string, Scopes ...string) (*Character, error) {
	character := new(Character)
	character.store = s
	query := make(map[string]interface{}, 0)
	if characterID > 0 {
		query["character_id"] = characterID
	}
	if characterName != "" {
		query["character_name"] = characterName
	}
	if Owner != "" {
		query["owner"] = Owner
	}
	if profileID != uuid.Nil {
		query["profile_id"] = profileID
	}
	if len(query) == 0 {
		return nil, ErrNoQuery
	}
	result := s.db.Where(query).Scopes(WithScopes(Scopes...)).First(character)
	if result.Error != nil {
		return nil, result.Error
	}
	return character, nil
}

func (s *Store) CreatePKCE(profile *Profile) (*PKCE, error) {
	pkce := MakePKCE(s, profile)
	result := s.db.Create(pkce)
	return pkce, result.Error
}

func (s *Store) FindPKCE(state string) (*PKCE, error) {
	pkce := new(PKCE)
	pkce.store = s
	result := s.db.Where("State = ?", state).First(pkce)
	return pkce, result.Error
}

func (s *Store) DeletePKCE(state string) error {
	pkce := new(PKCE)
	result := s.db.Where("State = ?", state).First(pkce)
	if result.Error != nil {
		return result.Error
	}
	result = s.db.Delete(pkce)
	return result.Error
}
