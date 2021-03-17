package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"time"

	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso/datastore"
)

type OAuthAutoConfig struct {
	Issuer                                     string   `json:"issuer,omitempty"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                              string   `json:"token_endpoint,omitempty"`
	ResponseTypesSupported                     []string `json:"response_types_supported,omitempty"`
	JwksURI                                    string   `json:"jwks_uri,omitempty"`
	RevocationEndpoint                         string   `json:"revocation_endpoint,omitempty"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported,omitempty"`
	refresher                                  *jwk.AutoRefresh
	cfg                                        *Config
}

func AutoConfig(cfgpath string) *OAuthAutoConfig {
	item := new(OAuthAutoConfig)
	item.refresher = jwk.NewAutoRefresh(context.TODO())
	item.cfg = new(Config)
	if err := item.cfg.Load(cfgpath, zap.L()); err != nil {
		logrus.WithError(err).Fatal("unable to load config", zap.String("cfgpath", cfgpath))
	}
	issuer, err := url.Parse(path.Join(CONST_ISSUER, CONST_AUTOCONFIG_URL))
	if err != nil {
		logrus.WithError(err).Fatal("unable to parse autoconfig url")
	}
	issuer.Scheme = "https"
	resp, err := ssoClient.Get(issuer.String())
	if err != nil {
		logrus.WithError(err).Fatal("unable to fetch autoconfig url")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.WithError(err).Fatal("unable to read autoconfig response")
	}
	if err := json.Unmarshal(data, &item); err != nil {
		logrus.WithError(err).Fatal("unable to deserialize autoconfig response")
	}
	item.refresher.Configure(item.JwksURI, jwk.WithHTTPClient(ssoClient), jwk.WithRefreshInterval(5*time.Minute))
	return item
}

func (r *OAuthAutoConfig) AppConfig() *Config {
	return r.cfg
}

func (r *OAuthAutoConfig) Oauth2Config(scopes ...string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     r.cfg.Key,
		ClientSecret: r.cfg.Secret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   r.AuthorizationEndpoint,
			TokenURL:  r.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: r.cfg.Callback,
		Scopes:      scopes,
	}
}

func (r *OAuthAutoConfig) JWKSet() (jwk.Set, error) {
	return r.refresher.Fetch(context.TODO(), r.JwksURI)
}

func (r *OAuthAutoConfig) JWT(token *oauth2.Token) (jwt.Token, error) {
	set, err := r.JWKSet()
	if err != nil {
		logrus.WithError(err).Fatal("Unable to fetch JWKKeys")
	}
	return jwt.Parse([]byte(token.AccessToken), jwt.WithKeySet(set))
}

func (r *OAuthAutoConfig) ValidateToken(t jwt.Token, CharacterID int64, Owner string) error {
	return jwt.Validate(t, jwt.WithIssuer(CONST_ISSUER), jwt.WithAudience(r.cfg.Key), jwt.WithSubject(fmt.Sprintf("EVE:CHARACTER:%d", CharacterID)), jwt.WithClaimValue("owner", Owner))
}

func (r *OAuthAutoConfig) TokenSource(store *datastore.DataStore, CharacterName string, scopes ...string) *EVETokenSource {
	return newEVETokenSource(r.Oauth2Config(scopes...), r, store, CharacterName)
}
