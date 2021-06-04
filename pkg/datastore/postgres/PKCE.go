package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v4"

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

	CreatedAt time.Time `json:"created_at" db:"created_at"`
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
	return p.store.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "DELETE FROM pkces WHERE id = $1"
			if _, err := tx.Exec(ctx, q, p.ID); err != nil {
				return err
			}
			logr.FromContextOrDiscard(ctx).Info(q, "id", p.ID, "profile", p.ProfileReference)
			//_, err := tx.ExecContext(ctx, q, p.ID)
			//return err
			return nil
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
		ID:                  uuid.Must(uuid.NewV4()),
		ProfileReference:    profile.ID,
		State:               uuid.Must(uuid.NewV4()),
		CodeVerifier:        encodedVerifier,
		CodeChallange:       challange,
		CodeChallangeMethod: "S256",
		CreatedAt:           time.Now(),
	}
}

var _ evesso.PKCE = &PKCE{}
