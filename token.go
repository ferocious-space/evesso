package evesso

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/gofrs/uuid"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

type ssoTokenSource struct {
	sync.RWMutex
	token *oauth2.Token

	ctx         context.Context
	jwkfn       func() (jwk.Set, error)
	oauthConfig *oauth2.Config

	store DataStore

	profileID     uuid.UUID
	characterName string
}

func (o *ssoTokenSource) GetCharacter() (Character, error) {
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return nil, err
	}
	character, err := profile.GetCharacter(o.ctx, 0, o.characterName, "", o.oauthConfig.Scopes)
	if err != nil {
		return nil, err
	}
	return character, nil
}

func (o *ssoTokenSource) jwt(token *oauth2.Token) (jwt.Token, error) {
	ks, err := o.jwkfn()
	if err != nil {
		return nil, err
	}

	jt, err := jwt.ParseString(token.AccessToken, jwt.WithKeySet(ks))
	if err != nil {
		return nil, err
	}

	err = o.validate(jt)
	if err != nil {
		return nil, err
	}
	return jt, nil
}

func (o *ssoTokenSource) validate(token jwt.Token) error {
	character, err := o.GetCharacter()
	if err != nil {
		return err
	}
	return jwt.Validate(
		token,
		jwt.WithIssuer(CONST_ISSUER), jwt.WithClaimValue("azp", o.oauthConfig.ClientID),
		jwt.WithSubject(fmt.Sprintf("CHARACTER:EVE:%d", character.GetCharacterID())), jwt.WithClaimValue("owner", character.GetOwner()),
	)
}

func (o *ssoTokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.token == nil {
		character, err := o.GetCharacter()
		if err != nil {
			return nil, err
		}
		token, err := character.Token()
		if err != nil {
			return nil, err
		}
		o.token = token
	}
	// get token from refresh token or refresh existing access token
	l, err := o.oauthConfig.TokenSource(o.ctx, o.token).Token()
	if err != nil {
		if o.token != nil {
			character, cerr := o.GetCharacter()
			if cerr != nil {
				return nil, errors.Wrap(cerr, err.Error())
			}
			terr := character.UpdateActiveState(o.ctx, false)
			if terr != nil {
				return nil, errors.Wrap(terr, err.Error())
			}
		}
		return nil, err
	}
	// check if refresh token changed
	if o.token.RefreshToken != l.RefreshToken {
		character, err := o.GetCharacter()
		if err != nil {
			return nil, err
		}
		err = character.UpdateToken(o.ctx, l.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	// verify token if changed
	if o.token.AccessToken != l.AccessToken {
		_, err := o.jwt(l)
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

func (o *ssoTokenSource) Save(token *oauth2.Token) error {
	o.Lock()
	defer o.Unlock()
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return err
	}
	_, err = profile.CreateCharacter(o.ctx, token)
	if err != nil {
		return err
	}
	o.token = token
	return nil
}

func (o *ssoTokenSource) AuthUrl() (string, error) {
	profile, err := o.store.GetProfile(o.ctx, o.profileID)
	if err != nil {
		return "", err
	}
	pkce, err := profile.CreatePKCE(o.ctx)
	if err != nil {
		return "", err
	}
	return o.oauthConfig.AuthCodeURL(
		pkce.GetState().String(),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challange", pkce.GetCodeChallange()),
		oauth2.SetAuthURLParam("code_challange_method", pkce.GetCodeChallangeMethod()),
	), nil
}

func (o *ssoTokenSource) AuthenticateRequest(request runtime.ClientRequest, _ strfmt.Registry) error {
	if t, e := o.Token(); e != nil {
		return e
	} else {
		return request.SetHeaderParam("Authorization", "Bearer "+t.AccessToken)
	}
}
