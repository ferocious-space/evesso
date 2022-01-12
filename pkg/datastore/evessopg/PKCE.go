package evessopg

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgtype"

	"github.com/ferocious-space/evesso"
)

type PKCE struct {
	store *PGStore `db:"-"`

	ID pgtype.UUID `json:"id" db:"id"`

	ProfileReference pgtype.UUID `json:"profile_id" db:"profile_ref"`

	State               pgtype.UUID      `json:"state" db:"state"`
	CodeVerifier        pgtype.Text      `json:"code_verifier" db:"code_verifier"`
	CodeChallange       pgtype.Text      `json:"code_challange" db:"code_challange"`
	CodeChallangeMethod pgtype.Text      `json:"code_challange_method" db:"code_challange_method"`
	Scopes              pgtype.TextArray `json:"scopes" db:"scopes"`
	ReferenceData       pgtype.JSONB     `json:"reference_data" db:"reference_data"`

	CreatedAt pgtype.Timestamptz `json:"created_at" db:"created_at"`
}

func (p *PKCE) GetReferenceData() interface{} {
	return p.ReferenceData.Get()
}

func (p *PKCE) GetScopes() []string {
	out := []string{}
	_ = p.Scopes.AssignTo(&out)
	return out
}

func (p *PKCE) GetID() uuid.UUID {
	cid := []byte{}
	err := p.ID.AssignTo(&cid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(cid)
}

func (p *PKCE) GetProfileID() uuid.UUID {
	cid := []byte{}
	err := p.ProfileReference.AssignTo(&cid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(cid)
}

func (p *PKCE) GetState() uuid.UUID {
	cid := []byte{}
	err := p.State.AssignTo(&cid)
	if err != nil {
		return uuid.Nil
	}
	return uuid.FromBytesOrNil(cid)
}

func (p *PKCE) GetCodeVerifier() string {
	out := ""
	_ = p.CodeVerifier.AssignTo(&out)
	return out
}

func (p *PKCE) GetCodeChallange() string {
	out := ""
	_ = p.CodeChallange.AssignTo(&out)
	return out
}

func (p *PKCE) GetCodeChallangeMethod() string {
	out := ""
	_ = p.CodeChallangeMethod.AssignTo(&out)
	return out
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
	t := time.Time{}
	err := p.CreatedAt.AssignTo(&t)
	if err != nil {
		return time.Time{}
	}
	return t
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
	pkce := &PKCE{
		ProfileReference: profile.ID,
	}
	if err := pkce.CreatedAt.Set(time.Now()); err != nil {
		return nil
	}
	if err := pkce.CodeVerifier.Set(encodedVerifier); err != nil {
		return nil
	}
	if err := pkce.CodeChallange.Set(challange); err != nil {
		return nil
	}
	if err := pkce.CodeChallangeMethod.Set("S256"); err != nil {
		return nil
	}
	return pkce
}

var _ evesso.PKCE = &PKCE{}
