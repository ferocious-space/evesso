package evesso

import (
	"context"
	"fmt"
	"sync"

	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/ferocious-space/evesso/datastore"
)

type ssoTokenSource struct {
	sync.RWMutex
	t *oauth2.Token

	ctx         context.Context
	jwkfn       func() (jwk.Set, error)
	oauthConfig *oauth2.Config

	store datastore.DataStore

	Profile       *datastore.Profile
	CharacterName string

	CharacterId int32
	owner       string
}

func (o *ssoTokenSource) JWT(token *oauth2.Token) (jwt.Token, error) {
	ks, err := o.jwkfn()
	if err != nil {
		return nil, err
	}
	jt, err := jwt.ParseString(token.AccessToken, jwt.WithKeySet(ks))
	if err != nil {
		return nil, err
	}
	return jt, o.validate(jt)
}

func (o *ssoTokenSource) validate(token jwt.Token) error {
	return jwt.Validate(
		token,
		jwt.WithIssuer(CONST_ISSUER), jwt.WithClaimValue("azp", o.oauthConfig.ClientID),
		jwt.WithSubject(fmt.Sprintf("CHARACTER:EVE:%d", o.CharacterId)), jwt.WithClaimValue("owner", o.owner),
	)
}

func (o *ssoTokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.t == nil {
		// get token from store , this should happen only on initial request
		data, err := o.store.FindCharacter(o.Profile.ID, o.CharacterId, o.CharacterName, o.owner, o.oauthConfig.Scopes)
		if err != nil {
			return nil, err
		}
		o.t = data.Token()
		o.CharacterId = data.CharacterID
		o.owner = data.Owner
	}
	// get token from refresh token or refresh existing access token
	l, err := o.oauthConfig.TokenSource(o.ctx, o.t).Token()
	if err != nil {
		return nil, err
	}
	// check if refresh token changed
	if o.t.RefreshToken != l.RefreshToken {
		data, err := datastore.ParseToken(l)
		if err != nil {
			return nil, err
		}
		char, err := o.store.FindCharacter(o.Profile.ID, data.CharacterID, data.CharacterName, data.Owner, data.Scopes)
		if err != nil {
			return nil, err
		}
		err = char.Update(l.RefreshToken, nil)
		if err != nil {
			return nil, err
		}
	}
	// verify token if changed
	if o.t.AccessToken != l.AccessToken {
		_, err := o.JWT(l)
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
	character, err := datastore.ParseToken(token)
	if err != nil {
		return err
	}
	err = o.store.CreateCharacter(o.Profile.ID, o.Profile.ProfileName, character)
	if err != nil {
		return err
	}
	o.t = token
	o.CharacterId = character.CharacterID
	o.owner = character.Owner
	return nil
}

func (o *ssoTokenSource) AuthUrl() (string, error) {
	pkce, err := o.Profile.MakePKCE()
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

func (o *ssoTokenSource) LocalhostCallback(fn func(url string) (*oauth2.Token, error)) error {
	if !o.Valid() {
		u, e := o.AuthUrl()
		if e != nil {
			return e
		}
		t, e := fn(u)
		if e != nil {
			return e
		}
		return o.Save(t)
	}
	return nil
}

func (o *ssoTokenSource) AuthenticateRequest(request runtime.ClientRequest, _ strfmt.Registry) error {
	if t, e := o.Token(); e != nil {
		return e
	} else {
		return request.SetHeaderParam("Authorization", "Bearer "+t.AccessToken)
	}
}
