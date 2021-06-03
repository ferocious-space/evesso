package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"

	"github.com/ferocious-space/evesso"
)

type PKCE struct {
	persister *PGStore `db:"-"`

	ID string `json:"id" db:"id"`

	ProfileReference evesso.ProfileID `json:"profile_id" db:"profile_ref"`

	State               string `json:"state" db:"state"`
	CodeVerifier        string `json:"code_verifier" db:"code_verifier"`
	CodeChallange       string `json:"code_challange" db:"code_challange"`
	CodeChallangeMethod string `json:"code_challange_method" db:"code_challange_method"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

func (p *PKCE) GetID() string {
	return p.ID
}

func (p *PKCE) GetProfileID() evesso.ProfileID {
	return p.ProfileReference
}

func (p *PKCE) GetState() string {
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
	return p.persister.GetProfile(ctx, p.GetProfileID())
}

func (p *PKCE) Destroy(ctx context.Context) error {
	return p.persister.transaction(
		ctx, func(ctx context.Context, tx pgx.Tx) error {
			q := "DELETE FROM pkces WHERE id = $1"
			if _, err := tx.Exec(ctx, q, p.ID); err != nil {
				return err
			}
			//q := tx.Rebind("delete from pkces where id = ?")
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
		ID:                  uuid.NewString(),
		ProfileReference:    profile.ID,
		State:               uuid.NewString(),
		CodeVerifier:        encodedVerifier,
		CodeChallange:       challange,
		CodeChallangeMethod: "S256",
		CreatedAt:           time.Now(),
	}
}

var _ evesso.PKCE = &PKCE{}
