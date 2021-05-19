package evesso

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso/pkg/datastore"
)

type ssoTokenSource struct {
	sync.RWMutex
	t *oauth2.Token

	ctx         context.Context
	jwkfn       func() (jwk.Set, error)
	oauthConfig *oauth2.Config

	store datastore.DataStore

	Profile       *datastore.Profile
	Character     *datastore.Character
	CharacterName string
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
	return jwt.Validate(
		token,
		jwt.WithIssuer(CONST_ISSUER), jwt.WithClaimValue("azp", o.oauthConfig.ClientID),
		jwt.WithSubject(fmt.Sprintf("CHARACTER:EVE:%d", o.Character.CharacterID)), jwt.WithClaimValue("owner", o.Character.Owner),
	)
}

func (o *ssoTokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.t == nil {
		// get token from store , this should happen only on initial request
		character, err := o.Profile.GetCharacter(o.ctx, 0, o.CharacterName, "", o.oauthConfig.Scopes)
		if err != nil {
			return nil, err
		}
		o.t, _ = character.Token()
		o.Character = character
	}
	// get token from refresh token or refresh existing access token
	l, err := o.oauthConfig.TokenSource(o.ctx, o.t).Token()
	if err != nil {
		if o.t != nil {
			terr := o.Character.UpdateActiveState(o.ctx, false)
			if terr != nil {
				return nil, errors.Wrap(terr, err.Error())
			}
		}
		return nil, err
	}
	// check if refresh token changed
	if o.t.RefreshToken != l.RefreshToken {
		err := o.Character.UpdateToken(o.ctx, l.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	// verify token if changed
	if o.t.AccessToken != l.AccessToken {
		_, err := o.jwt(l)
		if err != nil {
			return nil, err
		}
		o.t = l
	}
	return o.t, nil
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
	character, err := o.Profile.CreateCharacter(o.ctx, token)
	if err != nil {
		return err
	}
	o.t = token
	o.Character = character
	return nil
}

func (o *ssoTokenSource) AuthUrl() (string, error) {
	pkce, err := o.Profile.CreatePKCE(o.ctx)
	if err != nil {
		return "", err
	}
	return o.oauthConfig.AuthCodeURL(
		pkce.State,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challange", pkce.CodeChallange),
		oauth2.SetAuthURLParam("code_challange_method", pkce.CodeChallangeMethod),
	), nil
}

func (o *ssoTokenSource) AuthenticateRequest(request runtime.ClientRequest, _ strfmt.Registry) error {
	if t, e := o.Token(); e != nil {
		return e
	} else {
		return request.SetHeaderParam("Authorization", "Bearer "+t.AccessToken)
	}
}
