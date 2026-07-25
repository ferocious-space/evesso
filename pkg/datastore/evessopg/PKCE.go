package evessopg

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/goccy/go-json"
	"github.com/google/uuid"

	"github.com/ferocious-space/evesso"
)

type PKCE struct {
	store *PGStore `db:"-"`

	ID uuid.UUID `json:"id" db:"id"`

	ProfileReference uuid.UUID `json:"profile_id" db:"profile_ref"`

	State               uuid.UUID `json:"state" db:"state"`
	CodeVerifier        string    `json:"code_verifier" db:"code_verifier"`
	CodeChallange       string    `json:"code_challange" db:"code_challange"`
	CodeChallangeMethod string    `json:"code_challange_method" db:"code_challange_method"`
	Scopes              []string  `json:"scopes" db:"scopes"`
	ReferenceData       []byte    `json:"reference_data" db:"reference_data"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

func (p *PKCE) GetReferenceData() interface{} {
	if len(p.ReferenceData) == 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(p.ReferenceData, &out); err != nil {
		return nil
	}
	return out
}

func (p *PKCE) GetScopes() []string {
	return p.Scopes
}

func (p *PKCE) GetID() uuid.UUID {
	return p.ID
}

func (p *PKCE) GetProfileID() uuid.UUID {
	return p.ProfileReference
}

func (p *PKCE) GetState() uuid.UUID {
	return p.State
}

func (p *PKCE) GetCodeVerifier() string {
	return p.CodeVerifier
}

func (p *PKCE) GetCodeChallange() string {
	return p.CodeChallange
}

func (p *PKCE) GetCodeChallangeMethod() string {
	return p.CodeChallangeMethod
}

func (p *PKCE) GetProfile(ctx context.Context) (evesso.Profile, error) {
	return p.store.GetProfile(ctx, p.GetProfileID())
}

func (p *PKCE) Destroy(ctx context.Context) error {
	err := p.store.Query(ctx, sq.Delete("evesso.pkces").Where(sq.Eq{"id": p.ID}), nil)
	if err != nil {
		return err
	}
	return nil
}

func (p *PKCE) Time() time.Time {
	return p.CreatedAt
}

func MakePKCE(profile *Profile) *PKCE {
	verifier := make([]byte, 32) //nolint:gomnd
	if n, err := rand.Read(verifier); err != nil || n != 32 {
		return nil
	}

	encodedVerifier := base64.RawURLEncoding.EncodeToString(verifier)
	shaEncodedVerifier := sha256.Sum256([]byte(encodedVerifier))
	challange := base64.RawURLEncoding.EncodeToString(shaEncodedVerifier[:])
	pkce := &PKCE{
		ProfileReference:    profile.ID,
		CreatedAt:           time.Now(),
		CodeVerifier:        encodedVerifier,
		CodeChallange:       challange,
		CodeChallangeMethod: "S256",
	}
	return pkce
}

var _ evesso.PKCE = &PKCE{}
