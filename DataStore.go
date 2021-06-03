package evesso

import (
	"context"
	"time"

	"golang.org/x/oauth2"
)

type DataStore interface {
	Setup(ctx context.Context, dsn string) error
	ProfileStore
}

type ProfileName string
type ProfileID string

type ProfileStore interface {
	NewProfile(ctx context.Context, profileName ProfileName) (Profile, error)
	GetProfile(ctx context.Context, profileID ProfileID) (Profile, error)
	FindProfile(ctx context.Context, profileName ProfileName) (Profile, error)
	DeleteProfile(ctx context.Context, profileID ProfileID) error

	GetPKCE(ctx context.Context, state string) (PKCE, error)
	CleanPKCE(ctx context.Context) error
}

type Profile interface {
	GetID() ProfileID
	GetName() ProfileName

	GetCharacter(ctx context.Context, characterID int32, characterName string, Owner string, Scopes []string) (Character, error)
	CreateCharacter(ctx context.Context, token *oauth2.Token) (Character, error)
	CreatePKCE(ctx context.Context) (PKCE, error)
	Delete(ctx context.Context) error
}

type PKCE interface {
	GetID() string
	GetProfileID() ProfileID
	GetState() string
	GetCodeVerifier() string
	GetCodeChallange() string
	GetCodeChallangeMethod() string

	GetProfile(ctx context.Context) (Profile, error)
	Destroy(ctx context.Context) error
	Time() time.Time
}

type Character interface {
	GetID() string
	GetScopes() []string
	GetProfileID() ProfileID
	GetCharacterName() string
	GetCharacterID() int32
	GetOwner() string
	IsActive() bool

	GetProfile(ctx context.Context) (Profile, error)
	UpdateToken(ctx context.Context, RefreshToken string) error
	UpdateActiveState(ctx context.Context, active bool) error
	Token() (*oauth2.Token, error)
	Delete(ctx context.Context) error
}

func MatchScopes(x, y []string) bool {
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

func elementsMatchByLoop(x, y []string) bool {
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

func elementsMatchByMap(x, y []string) bool {
	// create a map of string -> int
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		// 0 value for int is 0, so just increment a counter for the string
		diff[_x]++
	}
	for _, _y := range y {
		// If the string _y is not in diff bail out early
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y] -= 1
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	if len(diff) == 0 {
		return true
	}
	return false
}
