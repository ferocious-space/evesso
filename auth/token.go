package auth

import (
	"context"
	"sync"

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
	store         *datastore.DataStore
	CharacterName string
	CharacterID   int32
}

func newEVETokenSource(ctx context.Context, ocfg *oauth2.Config, acfg *OAuthAutoConfig, tstore *datastore.DataStore, characterName string) *EVETokenSource {
	return &EVETokenSource{ctx: ctx, ocfg: ocfg, acfg: acfg, CharacterName: characterName, store: tstore, t: nil, jt: nil}
}

func (o *EVETokenSource) Token() (*oauth2.Token, error) {
	o.Lock()
	defer o.Unlock()
	if o.t == nil {
		// get token from store , this should happen only on initial request
		id, token, err := o.store.GetToken(o.CharacterName, o.ocfg.Scopes...)
		if err != nil {
			return nil, err
		}
		o.t = token
		o.CharacterID = id
	}
	// get token from refresh token or refresh existing access token
	l, err := o.ocfg.TokenSource(context.WithValue(o.ctx, oauth2.HTTPClient, o.acfg.SSOHttpClient), o.t).Token()
	if err != nil {
		return nil, err
	}
	// check if refresh token changed
	if o.t.RefreshToken != l.RefreshToken {
		o.Save(l)
	}

	// verify token if changed
	if o.t.AccessToken != l.AccessToken {
		jwtToken, err := o.acfg.JWT(o.ctx, l)
		if err != nil {
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

	id, err := o.store.SaveToken(token)
	if err != nil {
		return err
	}
	o.t = token
	o.CharacterID = id
	return nil
}

func (o *EVETokenSource) AuthInfoWriter() runtime.ClientAuthInfoWriter {
	return runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
		if t, e := o.Token(); e != nil {
			return e
		} else {
			return r.SetHeaderParam("Authorization", "Bearer "+t.AccessToken)
		}
	})
}
