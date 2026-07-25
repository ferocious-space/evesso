package evesso

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

var (
	ErrTokenScope = errors.New("scope is missing")
	ErrTokenName  = errors.New("name is missing")
	ErrTokenOwner = errors.New("owner is missing")
	ErrTokenID    = errors.New("id is missing")
)

// CharacterClaims is the identity a character record is created from. Its fields
// are unexported and the only constructor is newCharacterClaims, which takes a
// jwt.Token that validateAccessToken has already checked against the SSO JWKS.
// A DataStore therefore cannot be handed an identity nobody verified.
type CharacterClaims struct {
	characterID   int32
	characterName string
	owner         string
	scopes        []string
}

func (c CharacterClaims) CharacterID() int32    { return c.characterID }
func (c CharacterClaims) CharacterName() string { return c.characterName }
func (c CharacterClaims) Owner() string         { return c.owner }
func (c CharacterClaims) Scopes() []string      { return c.scopes }

type DataStore interface {
	Setup(ctx context.Context, dsn string) error

	NewProfile(ctx context.Context, profileName string, data interface{}) (Profile, error)

	AllProfiles(ctx context.Context) ([]Profile, error)
	GetProfile(ctx context.Context, profileID uuid.UUID) (Profile, error)
	FindProfile(ctx context.Context, profileName string) (Profile, error)
	FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string) (Profile, Character, error)
	DeleteProfile(ctx context.Context, profileID uuid.UUID) error

	GetPKCE(ctx context.Context, pkceID uuid.UUID) (PKCE, error)
	FindPKCE(ctx context.Context, state uuid.UUID) (PKCE, error)
	CleanPKCE(ctx context.Context) error
}

type Profile interface {
	GetID() uuid.UUID
	GetName() string
	GetData() any

	// Rename changes the profile name. Names are unique, so renaming onto one
	// that is already taken fails.
	Rename(ctx context.Context, name string) error

	AllCharacters(ctx context.Context) ([]Character, error)
	GetCharacter(ctx context.Context, uuid uuid.UUID) (Character, error)
	FindCharacter(ctx context.Context, characterID int32, characterName string, Owner string, Scopes []string) (Character, error)

	// CreateCharacter persists claims that the caller has already verified. It
	// must not re-derive identity from token.AccessToken.
	CreateCharacter(ctx context.Context, claims CharacterClaims, token *oauth2.Token, referenceData interface{}) (Character, error)
	CreatePKCE(ctx context.Context, referenceData interface{}, scopes ...string) (PKCE, error)
	Delete(ctx context.Context) error
}

type PKCE interface {
	GetID() uuid.UUID
	GetProfileID() uuid.UUID
	GetState() uuid.UUID
	GetCodeVerifier() string
	GetCodeChallange() string
	GetCodeChallangeMethod() string
	GetScopes() []string
	GetReferenceData() interface{}

	GetProfile(ctx context.Context) (Profile, error)
	Destroy(ctx context.Context) error
	Time() time.Time
}

type Character interface {
	GetID() uuid.UUID
	GetProfileID() uuid.UUID
	GetCharacterName() string
	GetCharacterID() int32
	GetOwner() string
	GetScopes() []string
	GetReferenceData() interface{}
	IsActive() bool

	GetProfile(ctx context.Context) (Profile, error)

	UpdateAccessToken(ctx context.Context, AccessToken string) error
	UpdateRefreshToken(ctx context.Context, RefreshToken string) error
	UpdateActiveState(ctx context.Context, active bool) error
	Token() (*oauth2.Token, error)
	Delete(ctx context.Context) error
}

func MatchScopes[T comparable](x, y []T) bool {
	xLen := len(x)
	yLen := len(y)
	if xLen != yLen {
		return false
	}
	if xLen > 20 {
		return elementsMatchByMap(x, y)
	} else {
		return elementsMatchByLoop(x, y)
	}
}

func elementsMatchByLoop[T comparable](x, y []T) bool {
	xLen := len(x)
	yLen := len(y)
	visited := make([]bool, yLen)
	for i := 0; i < xLen; i++ {
		found := false
		element := x[i]
		for j := 0; j < yLen; j++ {
			if visited[j] {
				continue
			}
			if element == y[j] {
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
func elementsMatchByMap[T comparable](x, y []T) bool {
	// create a map of string -> int
	diff := make(map[T]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y]--
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	return len(diff) == 0
}
