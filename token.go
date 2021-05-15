package evesso

import (
	"sync"

	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/ferocious-space/evesso/datastore"
)

type ssoTokenSource struct {
	sync.RWMutex
	t     *oauth2.Token
	jt    jwt.Token
	ocfg  *oauth2.Config
	acfg  *autoConfig
	store datastore.DataStore

	Profile       *datastore.Profile
	CharacterName string

	CharacterId int32
	owner       string
}

func (o *ssoTokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.t == nil {
		// get token from store , this should happen only on initial request
		data, err := o.store.FindCharacter(o.Profile.ID, o.CharacterId, o.CharacterName, o.owner, o.ocfg.Scopes...)
		if err != nil {
			return nil, err
		}
		o.t = data.Token()
		o.CharacterId = data.CharacterID
		o.owner = data.Owner
	}
	// get token from refresh token or refresh existing access token
	l, err := o.ocfg.TokenSource(o.acfg.SSOCTX, o.t).Token()
	if err != nil {
		return nil, err
	}
	// check if refresh token changed
	if o.t.RefreshToken != l.RefreshToken {
		data, err := datastore.ParseToken(l)
		if err != nil {
			return nil, err
		}

		err = o.Profile.CreateCharacter(data)
		if err != nil {
			return nil, err
		}
		o.CharacterId = data.CharacterID
		o.owner = data.Owner
	}
	// verify token if changed
	if o.t.AccessToken != l.AccessToken {
		jwtToken, err := o.acfg.JWT(l)
		if err != nil {
			return nil, err
		}
		if err := o.acfg.ValidateToken(jwtToken, o.CharacterId, o.owner); err != nil {
			return nil, err
		}
		o.t = l
		o.jt = jwtToken
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
	err = o.Profile.CreateCharacter(character)
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
	return o.ocfg.AuthCodeURL(
		pkce.State,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challange", pkce.CodeChallange),
		oauth2.SetAuthURLParam("code_challange_method", pkce.CodeChallangeMethod),
	), nil
}

func (o *ssoTokenSource) LocalhostCallback() error {
	if !o.Valid() {
		u, e := o.AuthUrl()
		if e != nil {
			return e
		}
		_, e = o.acfg.LocalhostAuth(u)
		if e != nil {
			return e
		}
		//return o.Save(t)
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