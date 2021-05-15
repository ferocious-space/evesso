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

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/internal/utils"
)

type autoConfig struct {
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
	cfg                                        *appConfig
	SSOHttpClient                              *http.Client

	store  datastore.DataStore
	SSOCTX context.Context
}

func AutoConfig(ctx context.Context, store datastore.DataStore, cfgpath string, client *http.Client) (*autoConfig, error) {
	item := new(autoConfig)
	item.SSOHttpClient = client
	item.refresher = jwk.NewAutoRefresh(ctx)
	item.cfg = new(appConfig)
	item.SSOCTX = context.WithValue(ctx, oauth2.HTTPClient, client)
	item.store = store
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
	item.refresher.Configure(
		item.JwksURI,
		jwk.WithHTTPClient(item.SSOHttpClient),
		jwk.WithRefreshInterval(5*time.Minute),
	)
	_, err = item.JWKSet()
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *autoConfig) AppConfig() *appConfig {
	return r.cfg
}

func (r *autoConfig) OAuth2(scopes ...string) *oauth2.Config {
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

func (r *autoConfig) JWKSet() (jwk.Set, error) {
	return r.refresher.Fetch(r.SSOCTX, r.JwksURI)
}

func (r *autoConfig) JWT(token *oauth2.Token) (jwt.Token, error) {
	set, err := r.JWKSet()
	if err != nil {
		return nil, err
	}
	return jwt.Parse([]byte(token.AccessToken), jwt.WithKeySet(set))
}

func (r *autoConfig) ValidateToken(jwtToken jwt.Token, CharacterId int32, Owner string) error {
	return jwt.Validate(
		jwtToken, jwt.WithIssuer(CONST_ISSUER), jwt.WithClaimValue("azp", r.cfg.Key),
		jwt.WithSubject(fmt.Sprintf("CHARACTER:EVE:%d", CharacterId)), jwt.WithClaimValue("owner", Owner),
	)
}

func (r *autoConfig) TokenSource(ProfileName, CharacterName string, Scopes ...string) (*ssoTokenSource, error) {
	profile, err := r.store.FindProfile(uuid.Nil, ProfileName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			profile = new(datastore.Profile)
			profile.ProfileName = ProfileName
			e := r.store.CreateProfile(profile)
			if e != nil {
				return nil, errors.Wrapf(err, "creating profile: %w", e)
			}
		} else {
			return nil, err
		}
	}
	return &ssoTokenSource{
		t:             nil,
		jt:            nil,
		ocfg:          r.OAuth2(Scopes...),
		acfg:          r,
		store:         r.store,
		Profile:       profile,
		CharacterName: CharacterName,
	}, nil
}

func (r *autoConfig) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(w)
	code := req.FormValue("code")
	state := req.FormValue("state")
	pkce, err := r.store.FindPKCE(state)
	if err != nil {
		//we have no state for this request, discard it
		return
	}
	//delete the state as we are handling it at the moment
	err = r.store.DeletePKCE(state)
	if err != nil {
		encoder.Encode(err)
		return
	}
	//check if more than 5 mins passed
	if time.Since(pkce.Time()) > 5*time.Minute {
		encoder.Encode("PKCE timeout")
		return
	}

	//get the token
	token, err := r.OAuth2().Exchange(
		r.SSOCTX,
		code,
		oauth2.SetAuthURLParam("code_verifier", pkce.CodeVerifier),
	)
	if err != nil {
		//token exchange failed ?
		encoder.Encode(err)
		return
	}

	//extract character
	character, err := datastore.ParseToken(token)
	if err != nil {
		encoder.Encode(err)
		//token parse failed ?
		return
	}

	//getprofile
	profile, err := pkce.GetProfile()
	if err != nil {
		if errors.Is(err, datastore.ErrProfileNotFound) {
			//there is no profile create new one
			profile = new(datastore.Profile)
			profile.ProfileName = pkce.ProfileName
			err := r.store.CreateProfile(profile)
			if err != nil {
				encoder.Encode(err)
				return
			}
		} else {
			encoder.Encode(err)
			return
		}
	}

	// we have profile now , create the character in the profile
	err = profile.CreateCharacter(character)
	if err != nil {
		encoder.Encode(err)
		return
	}

	json.NewEncoder(w).Encode(profile)
	json.NewEncoder(w).Encode(character)
}

func (r *autoConfig) LocalhostAuth(urlPath string) (*oauth2.Token, error) {
	if err := utils.OSExec(urlPath); err != nil {
		return nil, err
	}

	callback, err := url.Parse(r.AppConfig().Callback)
	if err != nil {
		return nil, err
	}
	stopChannel := make(chan struct{}, 1)

	outToken := new(oauth2.Token)

	e := echo.New()
	e.HideBanner = true
	e.GET(
		callback.Path, func(c echo.Context) error {
			defer func() {
				stopChannel <- struct{}{}
			}()
			code := c.Request().FormValue("code")
			state := c.Request().FormValue("state")
			pkce, err := r.store.FindPKCE(state)
			if err != nil {
				//we have no state for this request, discard it
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}

			err = r.store.DeletePKCE(state)
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
				r.SSOCTX,
				code,
				oauth2.SetAuthURLParam("code_verifier", pkce.CodeVerifier),
			)

			if err != nil {
				return &echo.HTTPError{
					Code:     http.StatusInternalServerError,
					Message:  err.Error(),
					Internal: err,
				}
			}
			outToken = token
			return c.JSON(http.StatusOK, token)
		},
	)

	go func() {
		err = e.Start(fmt.Sprintf("%s:%s", callback.Hostname(), callback.Port()))
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()

	select {
	case <-stopChannel:
		err = e.Shutdown(ctx)
	case <-ctx.Done():
		err = e.Shutdown(ctx)
	}

	return outToken, err
}
