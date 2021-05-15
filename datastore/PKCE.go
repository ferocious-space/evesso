package datastore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var (
	ErrStateSuccess       = errors.New("successful resolution")
	ErrStateAlreadyExists = errors.New("state already exists")
	ErrStateNotFound      = errors.New("state not found")
)

type PKCE struct {
	store DataStore `gorm:"-"`

	State               string `json:"state" gorm:"primaryKey;"`
	CodeVerifier        string `json:"code_verifier"`
	CodeChallange       string `json:"code_challange"`
	CodeChallangeMethod string `json:"code_challange_method"`

	CreatedAt   time.Time `json:"created_at"`
	ProfileID   uuid.UUID `json:"profile_id"`
	ProfileName string    `json:"profile_name"`
}

func (p *PKCE) GetProfile() (*Profile, error) {
	return p.store.FindProfile(p.ProfileID, p.ProfileName)
}

func (p *PKCE) Time() time.Time {
	return p.CreatedAt
}

func MakePKCE(store DataStore, profile *Profile) *PKCE {
	sha := sha256.New()

	verifier := make([]byte, 32)
	if n, err := rand.Read(verifier); err != nil || n != 32 {
		return nil
	}

	encodedVerifier := base64.RawURLEncoding.EncodeToString(verifier)
	shaEncodedVerifier := sha.Sum([]byte(encodedVerifier))
	challange := base64.RawURLEncoding.EncodeToString(shaEncodedVerifier)
	return &PKCE{
		store: store,

		State:               uuid.New().String(),
		CodeVerifier:        encodedVerifier,
		CodeChallange:       challange,
		CodeChallangeMethod: "S256",

		CreatedAt:   time.Now(),
		ProfileID:   profile.ID,
		ProfileName: profile.ProfileName,
	}
}
