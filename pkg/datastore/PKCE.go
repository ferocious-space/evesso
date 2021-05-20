package datastore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/gobuffalo/pop/v5"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var (
	ErrStateSuccess       = errors.New("successful resolution")
	ErrStateAlreadyExists = errors.New("state already exists")
	ErrStateNotFound      = errors.New("state not found")
)

type PKCE struct {
	persister *Persister `db:"-"`

	ID string `json:"id" db:"id"`

	ProfileReference    string `json:"profile_id" db:"profile_ref"`
	State               string `json:"state" db:"state"`
	CodeVerifier        string `json:"code_verifier" db:"code_verifier"`
	CodeChallange       string `json:"code_challange" db:"code_challange"`
	CodeChallangeMethod string `json:"code_challange_method" db:"code_challange_method"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (p *PKCE) GetProfile(ctx context.Context) (*Profile, error) {
	return p.persister.GetProfile(ctx, p.ProfileReference)
}

func (p *PKCE) Destroy(ctx context.Context) error {
	return p.persister.tx(
		ctx, func(ctx context.Context, c *pop.Connection) error {
			return HandleError(p.persister.Connection(ctx).Destroy(p))
		},
	)
}

func (p *PKCE) Time() time.Time {
	return p.CreatedAt
}

func MakePKCE(profile *Profile) *PKCE {
	sha := sha256.New()

	verifier := make([]byte, 32)
	if n, err := rand.Read(verifier); err != nil || n != 32 {
		return nil
	}

	encodedVerifier := base64.RawURLEncoding.EncodeToString(verifier)
	shaEncodedVerifier := sha.Sum([]byte(encodedVerifier))
	challange := base64.RawURLEncoding.EncodeToString(shaEncodedVerifier)
	return &PKCE{
		ID:                  uuid.NewString(),
		ProfileReference:    profile.ID,
		State:               uuid.NewString(),
		CodeVerifier:        encodedVerifier,
		CodeChallange:       challange,
		CodeChallangeMethod: "S256",
	}
}
