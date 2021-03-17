package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/ferocious-space/durableclient"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
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
	SSOHttpClient                              *http.Client
}

func AutoConfig(ctx context.Context, cfgpath string, logger *zap.Logger) (*OAuthAutoConfig, error) {
	item := new(OAuthAutoConfig)
	item.SSOHttpClient = durableclient.NewClient("JWKS", "github.com/ferocious-space/evesso", logger)
	item.refresher = jwk.NewAutoRefresh(ctx)
	item.cfg = new(Config)
	if err := item.cfg.Load(cfgpath); err != nil {
		return nil, err
	}
	issuer, err := url.Parse(path.Join(CONST_ISSUER, CONST_AUTOCONFIG_URL))
	if err != nil {
		return nil, err
	}
	issuer.Scheme = "https"
	resp, err := item.SSOHttpClient.Get(issuer.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	item.refresher.Configure(item.JwksURI, jwk.WithHTTPClient(item.SSOHttpClient), jwk.WithRefreshInterval(5*time.Minute))
	_, err = item.JWKSet(ctx)
	if err != nil {
		return nil, err
	}
	return item, nil
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

func (r *OAuthAutoConfig) JWKSet(ctx context.Context) (jwk.Set, error) {
	return r.refresher.Fetch(ctx, r.JwksURI)
}

func (r *OAuthAutoConfig) JWT(ctx context.Context, token *oauth2.Token) (jwt.Token, error) {
	set, err := r.JWKSet(ctx)
	if err != nil {
		return nil, err
	}
	return jwt.Parse([]byte(token.AccessToken), jwt.WithKeySet(set))
}

func (r *OAuthAutoConfig) ValidateToken(t jwt.Token, CharacterID int32, Owner string) error {
	return jwt.Validate(t, jwt.WithIssuer(CONST_ISSUER), jwt.WithAudience(r.cfg.Key), jwt.WithSubject(fmt.Sprintf("EVE:CHARACTER:%d", CharacterID)), jwt.WithClaimValue("owner", Owner))
}

func (r *OAuthAutoConfig) TokenSource(ctx context.Context, store *datastore.DataStore, CharacterName string, scopes []string) *EVETokenSource {
	return newEVETokenSource(ctx, r.Oauth2Config(scopes...), r, store, CharacterName)
}
