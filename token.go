package evesso

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"golang.org/x/oauth2"
)

type ssoTokenSource struct {
	sync.RWMutex
	token *oauth2.Token

	ctx         context.Context
	jwkfn       func() (jwk.Set, error)
	oauthConfig *oauth2.Config

	store DataStore

	character     Character
	profileID     uuid.UUID
	characterName string
}

func (o *ssoTokenSource) GetCharacter() (Character, error) {
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return nil, err
	}
	character, err := profile.FindCharacter(o.ctx, 0, o.characterName, "", o.oauthConfig.Scopes)
	if err != nil {
		return nil, err
	}
	return character, nil
}

// validateAccessToken checks an SSO access token's signature against ks and its
// issuer and audience claims. It is the only place a token earns enough trust to
// have identity read out of it; see newCharacterClaims.
func validateAccessToken(ks jwk.Set, clientID, accessToken string) (jwt.Token, error) {
	return jwt.ParseString(
		accessToken,
		jwt.WithKeySet(ks),
		jwt.WithValidator(jwt.ValidatorFunc(func(_ context.Context, t jwt.Token) error {
			iss, ok := t.Issuer()
			if !ok {
				return fmt.Errorf("jwt: missing issuer claim")
			}
			for _, validIssuer := range VALID_ISSUERS {
				if iss == validIssuer {
					return nil
				}
			}
			return fmt.Errorf("jwt: invalid issuer %q", iss)
		})),
		jwt.WithAudience(AUDIENCE),
		jwt.WithAudience(clientID),
		jwt.WithAcceptableSkew(30*time.Second),
	)
}

// newCharacterClaims reads identity out of a token that validateAccessToken has
// already accepted. Passing an unverified token here defeats the point.
func newCharacterClaims(jt jwt.Token) (CharacterClaims, error) {
	var claims CharacterClaims

	var scp interface{}
	if err := jt.Get("scp", &scp); err != nil {
		return claims, ErrTokenScope
	}
	switch x := scp.(type) {
	case string:
		claims.scopes = []string{x}
	case []interface{}:
		for _, s := range x {
			str, ok := s.(string)
			if !ok {
				return claims, ErrTokenScope
			}
			claims.scopes = append(claims.scopes, str)
		}
	default:
		return claims, ErrTokenScope
	}

	if err := jt.Get("name", &claims.characterName); err != nil {
		return claims, ErrTokenName
	}
	if err := jt.Get("owner", &claims.owner); err != nil {
		return claims, ErrTokenOwner
	}
	subj, _ := jt.Subject()
	if n, err := fmt.Sscanf(subj, "CHARACTER:EVE:%d", &claims.characterID); err != nil || n != 1 {
		return claims, ErrTokenID
	}
	sort.Strings(claims.scopes)
	return claims, nil
}

func (o *ssoTokenSource) validate(token *oauth2.Token) (jwt.Token, error) {
	ks, err := o.jwkfn()
	if err != nil {
		return nil, err
	}
	return validateAccessToken(ks, o.oauthConfig.ClientID, token.AccessToken)
}

func (o *ssoTokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.token == nil {
		if o.character == nil {
			character, err := o.GetCharacter()
			if err != nil {
				return nil, err
			}
			o.character = character
		}
		token, err := o.character.Token()
		if err != nil {
			return nil, err
		}
		o.token = token
	}
	// get token from refresh token or refresh existing access token
	l, err := o.oauthConfig.TokenSource(o.ctx, o.token).Token()
	if err != nil {
		var retrieveError *oauth2.RetrieveError
		if errors.As(err, &retrieveError) {
			// mark character as inactive
			terr := o.character.UpdateActiveState(o.ctx, false)
			if terr != nil {
				return nil, fmt.Errorf("%s: %w", terr, err)
			}
			return nil, err
		}
		return nil, err
	}
	// check if refresh token changed
	if o.token.RefreshToken != l.RefreshToken {
		err = o.character.UpdateRefreshToken(o.ctx, l.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	// verify token if changed
	if o.token.AccessToken != l.AccessToken {
		_, err = o.validate(l)
		if err != nil {
			return nil, err
		}
		err = o.character.UpdateAccessToken(o.ctx, l.AccessToken)
		if err != nil {
			return nil, err
		}
		o.token = l
	}
	return o.token, nil
}

func (o *ssoTokenSource) Valid() bool {
	if _, err := o.Token(); err != nil {
		return false
	}
	return true
}

func (o *ssoTokenSource) Save(token *oauth2.Token, referenceData interface{}) error {
	o.Lock()
	defer o.Unlock()
	jt, err := o.validate(token)
	if err != nil {
		return err
	}
	claims, err := newCharacterClaims(jt)
	if err != nil {
		return err
	}
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return err
	}
	_, err = profile.CreateCharacter(o.ctx, claims, token, referenceData)
	if err != nil {
		return err
	}
	o.token = token
	return nil
}

func (o *ssoTokenSource) AuthURL(referenceData interface{}) (string, error) {
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return "", err
	}
	pkce, err := profile.CreatePKCE(o.ctx, referenceData, o.oauthConfig.Scopes...)
	if err != nil {
		return "", err
	}
	return o.oauthConfig.AuthCodeURL(
		pkce.GetState().String(),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", pkce.GetCodeChallange()),
		oauth2.SetAuthURLParam("code_challenge_method", pkce.GetCodeChallangeMethod()),
	), nil
}

// RequestEditor is compatible with github.com/ferocious-space/eveapi/esi.RequestEditorFn
// (func(ctx context.Context, req *http.Request) error): it sets the Authorization header
// on req using the current access token.
func (o *ssoTokenSource) RequestEditor(ctx context.Context, req *http.Request) error {
	t, err := o.Token()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	return nil
}
