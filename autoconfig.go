package evesso

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/go-logr/logr"
	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/pkg/errors"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso/internal/utils"
)

type EVESSO struct {
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

	refresher *jwk.AutoRefresh
	cfg       *appConfig
	client    *http.Client

	store DataStore
	ctx   context.Context
}

func AutoConfig(ctx context.Context, cfgpath string, store DataStore, client *http.Client) (*EVESSO, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	item := new(EVESSO)
	item.client = client
	item.refresher = jwk.NewAutoRefresh(ctx)
	item.cfg = new(appConfig)
	item.ctx = ctx
	if err := item.cfg.Load(cfgpath); err != nil {
		return nil, err
	}
	err := store.Setup(ctx, item.cfg.DSN)
	if err != nil {
		return nil, err
	}
	item.store = store
	issuer, err := url.Parse(path.Join(CONST_ISSUER, CONST_AUTOCONFIG_URL))
	if err != nil {
		return nil, err
	}
	issuer.Scheme = "https"
	resp, err := client.Get(issuer.String())
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
	item.refresher.Configure(
		item.JwksURI,
		jwk.WithHTTPClient(client),
		jwk.WithRefreshInterval(5*time.Minute),
	)
	return item, nil
}

func (r *EVESSO) AppConfig() *appConfig {
	return r.cfg
}

func (r *EVESSO) OAuth2(scopes ...string) *oauth2.Config {
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

func (r *EVESSO) Store() DataStore {
	return r.store
}

func (r *EVESSO) TokenSource(profileID uuid.UUID, CharacterName string, Scopes ...string) (*ssoTokenSource, error) {
	return &ssoTokenSource{
		token:       nil,
		ctx:         context.WithValue(r.ctx, oauth2.HTTPClient, r.client),
		oauthConfig: r.OAuth2(Scopes...),
		jwkfn: func() (jwk.Set, error) {
			return r.refresher.Fetch(r.ctx, r.JwksURI)
		},
		store:         r.store,
		profileID:     profileID,
		characterName: CharacterName,
	}, nil
}

func (r *EVESSO) CharacterSource(character Character) (*ssoTokenSource, error) {
	return &ssoTokenSource{
		token:       nil,
		ctx:         context.WithValue(r.ctx, oauth2.HTTPClient, r.client),
		oauthConfig: r.OAuth2(character.GetScopes()...),
		jwkfn: func() (jwk.Set, error) {
			return r.refresher.Fetch(r.ctx, r.JwksURI)
		},
		store:         r.store,
		profileID:     character.GetProfileID(),
		characterName: character.GetCharacterName(),
		character:     character,
	}, nil
}

func (r *EVESSO) AuthUrl(pkce PKCE) string {
	return r.OAuth2(pkce.GetScopes()...).AuthCodeURL(
		pkce.GetState().String(),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challange", pkce.GetCodeChallange()),
		oauth2.SetAuthURLParam("code_challange_method", pkce.GetCodeChallangeMethod()),
	)
}

func (r *EVESSO) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(w)
	code := req.FormValue("code")
	state := req.FormValue("state")
	pkce, err := r.store.FindPKCE(req.Context(), uuid.FromStringOrNil(state))
	if err != nil {
		//we have no state for this request, discard it
		return
	}
	profile, err := pkce.GetProfile(req.Context())
	if err != nil {
		return
	}
	//delete the state as we are handling it at the moment
	err = pkce.Destroy(req.Context())
	if err != nil {
		return
	}
	//check if more than 5 mins passed
	if time.Since(pkce.Time()) > 5*time.Minute {
		_ = encoder.Encode("authentication timeout, please try again")
		return
	}

	//get the token
	token, err := r.OAuth2().Exchange(
		r.ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", pkce.GetCodeVerifier()),
	)
	if err != nil {
		//token exchange failed ?
		_ = encoder.Encode(err)
		return
	}
	//extract character
	_, err = profile.CreateCharacter(req.Context(), token, pkce.GetReferenceData())
	if err != nil {
		_ = encoder.Encode(err)
		//token parse failed ?
		return
	}
	_ = r.store.CleanPKCE(context.TODO())
	http.Redirect(w, req, r.AppConfig().Redirect, http.StatusFound)
}

func (r *EVESSO) LocalhostAuth(urlPath string) error {
	if err := utils.OSExec(urlPath); err != nil {
		return err
	}

	callback, err := url.Parse(r.AppConfig().Callback)
	if err != nil {
		return err
	}
	stopChannel := make(chan struct{}, 1)
	errChannel := make(chan error, 1)

	e := echo.New()
	e.HideBanner = true
	e.GET(
		callback.Path, func(c echo.Context) error {
			defer func() {
				stopChannel <- struct{}{}
			}()

			ctx := logr.NewContext(c.Request().Context(), logr.FromContextOrDiscard(r.ctx))

			code := c.Request().FormValue("code")
			state := c.Request().FormValue("state")
			pkce, err := r.store.FindPKCE(ctx, uuid.FromStringOrNil(state))
			if err != nil {
				//we have no state for this request, discard it
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			profile, err := pkce.GetProfile(ctx)
			if err != nil {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			err = pkce.Destroy(ctx)
			if err != nil {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			//check if more than 5 mins passed
			if time.Since(pkce.Time()) > 5*time.Minute {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  "timeout ",
					Internal: err,
				}
			}

			token, err := r.OAuth2().Exchange(
				ctx,
				code,
				oauth2.SetAuthURLParam("code_verifier", pkce.GetCodeVerifier()),
			)

			if err != nil {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			_, err = profile.CreateCharacter(ctx, token, pkce.GetReferenceData())
			if err != nil {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			_ = r.store.CleanPKCE(ctx)

			return c.JSON(http.StatusOK, token)
		},
	)

	go func() {
		if callback.Port() == "" {
			if callback.Scheme == "http" {
				err = e.Start(":80")
			} else {
				if r.AppConfig().Autocert {
					e.AutoTLSManager.HostPolicy = autocert.HostWhitelist(callback.Hostname())
					e.AutoTLSManager.Cache = autocert.DirCache(r.AppConfig().AutocertCache)
					err = e.StartAutoTLS(":443")
				} else {
					err = e.StartTLS(":443", r.AppConfig().TLSCert, r.AppConfig().TLSKey)
				}

			}
		} else {
			err = e.Start(fmt.Sprintf(":%s", callback.Port()))
		}

		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errChannel <- err
	}()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()

	select {
	case err := <-errChannel:
		return err
	case <-stopChannel:
		err = e.Shutdown(ctx)
	case <-ctx.Done():
		err = e.Shutdown(ctx)
	}

	return err
}
