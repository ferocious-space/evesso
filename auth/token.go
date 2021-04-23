package auth

import (
	"context"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/oauth2"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/ferocious-space/evesso/datastore"
)

type EVETokenSource struct {
	sync.RWMutex
	ctx           context.Context
	t             *oauth2.Token
	jt            jwt.Token
	ocfg          *oauth2.Config
	acfg          *OAuthAutoConfig
	store         datastore.AccountStore
	CharacterName string
	CharacterId   int32
	owner         string
}

func newEVETokenSource(
	ctx context.Context,
	ocfg *oauth2.Config,
	acfg *OAuthAutoConfig,
	tstore datastore.AccountStore,
	characterName string,
) *EVETokenSource {
	return &EVETokenSource{
		ctx: ctx, ocfg: ocfg, acfg: acfg, CharacterName: characterName, store: tstore, t: nil, jt: nil,
	}
}

func (o *EVETokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.t == nil {
		// get token from store , this should happen only on initial request
		data, err := o.store.SearchName(o.CharacterName, o.ocfg.Scopes)
		if err != nil {
			return nil, err
		}
		o.t = &oauth2.Token{RefreshToken: data.RefreshToken, Expiry: time.Now()}
		o.CharacterId = data.CharacterId
		o.owner = data.Owner
	}
	// get token from refresh token or refresh existing access token
	l, err := o.ocfg.TokenSource(context.WithValue(o.ctx, oauth2.HTTPClient, o.acfg.SSOHttpClient), o.t).Token()
	if err != nil {
		return nil, err
	}
	// check if refresh token changed
	if o.t.RefreshToken != l.RefreshToken {
		data, err := datastore.NewAccountData(l)
		if err != nil {
			return nil, err
		}
		err = o.store.Update(data)
		if err != nil {
			return nil, err
		}
		o.CharacterId = data.CharacterId
		o.owner = data.Owner
	}
	// verify token if changed
	if o.t.AccessToken != l.AccessToken {
		jwtToken, err := o.acfg.JWT(o.ctx, l)
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

func (o *EVETokenSource) Valid() bool {
	if _, err := o.Token(); err != nil {
		return false
	}
	return true
}

func (o *EVETokenSource) Save(token *oauth2.Token) error {
	o.Lock()
	defer o.Unlock()
	data, err := datastore.NewAccountData(token)
	if err != nil {
		return err
	}
	err = o.store.Create(data)
	if err != nil {
		return err
	}
	o.t = token
	o.CharacterId = data.CharacterId
	return nil
}

func (o *EVETokenSource) AuthenticateRequest(request runtime.ClientRequest, _ strfmt.Registry) error {
	if t, e := o.Token(); e != nil {
		return e
	} else {
		return request.SetHeaderParam("Authorization", "Bearer "+t.AccessToken)
	}
}
