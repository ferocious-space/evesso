package evesso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
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

	refresher *jwk.Cache
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
	issuer, err := url.Parse(path.Join(ISSUER, AUTOCONFIG_URL))
	if err != nil {
		return nil, err
	}
	issuer.Scheme = "https"
	withContext, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(withContext)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	item.refresher, err = jwk.NewCache(ctx, httprc.NewClient(httprc.WithHTTPClient(client)))
	if err != nil {
		return nil, err
	}
	if err = item.refresher.Register(
		ctx, item.JwksURI,
		jwk.WithHTTPClient(client),
		jwk.WithConstantInterval(5*time.Minute),
	); err != nil {
		return nil, err
	}
	return item, nil
}
func (r *EVESSO) AppConfig() *appConfig {
	return r.cfg
}
func (r *EVESSO) oAuth2(scopes ...string) *oauth2.Config {
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

// verify checks an access token against the SSO JWKS. Character identity comes
// from the token it returns, never from an unverified parse of the same string.
func (r *EVESSO) verify(ctx context.Context, accessToken string) (jwt.Token, error) {
	ks, err := r.refresher.Lookup(ctx, r.JwksURI)
	if err != nil {
		return nil, err
	}
	return validateAccessToken(ks, r.cfg.Key, accessToken)
}
func (r *EVESSO) TokenSource(profileID uuid.UUID, CharacterName string, Scopes ...string) (*ssoTokenSource, error) {
	return &ssoTokenSource{
		token:       nil,
		ctx:         context.WithValue(r.ctx, oauth2.HTTPClient, r.client),
		oauthConfig: r.oAuth2(Scopes...),
		jwkfn: func() (jwk.Set, error) {
			return r.refresher.Lookup(r.ctx, r.JwksURI)
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
		oauthConfig: r.oAuth2(character.GetScopes()...),
		jwkfn: func() (jwk.Set, error) {
			return r.refresher.Lookup(r.ctx, r.JwksURI)
		},
		store:         r.store,
		profileID:     character.GetProfileID(),
		characterName: character.GetCharacterName(),
		character:     character,
	}, nil
}
func (r *EVESSO) AuthUrl(pkce PKCE) string {
	return r.oAuth2(pkce.GetScopes()...).AuthCodeURL(
		pkce.GetState().String(),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", pkce.GetCodeChallange()),
		oauth2.SetAuthURLParam("code_challenge_method", pkce.GetCodeChallangeMethod()),
	)
}
func (r *EVESSO) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(w)
	code := req.FormValue("code")
	state := req.FormValue("state")
	stateID, err := uuid.Parse(state)
	if err != nil {
		//we have no state for this request, discard it
		return
	}
	pkce, err := r.store.FindPKCE(req.Context(), stateID)
	if err != nil {
		//we have no state for this request, discard it
		return
	}
	profile, err := pkce.GetProfile(req.Context())
	if err != nil {
		return
	}
	// delete the state as we are handling it at the moment
	err = pkce.Destroy(req.Context())
	if err != nil {
		return
	}
	// check if more than 5 mins passed
	if time.Since(pkce.Time()) > 5*time.Minute {
		_ = encoder.Encode("authentication timeout, please try again")
		return
	}

	// get the token
	token, err := r.oAuth2().Exchange(
		r.ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", pkce.GetCodeVerifier()),
	)
	if err != nil {
		// token exchange failed ?
		_ = encoder.Encode(err)
		return
	}
	// extract character from the verified token
	jt, err := r.verify(req.Context(), token.AccessToken)
	if err != nil {
		_ = encoder.Encode(err)
		return
	}
	claims, err := newCharacterClaims(jt)
	if err != nil {
		_ = encoder.Encode(err)
		return
	}
	_, err = profile.CreateCharacter(req.Context(), claims, token, pkce.GetReferenceData())
	if err != nil {
		_ = encoder.Encode(err)
		return
	}
	_ = r.store.CleanPKCE(req.Context())
	//https://login.eveonline.com/Account/LogOff?ReturnUrl=https%3A%2F%2Fwww.fuzzwork.co.uk%2Fauth/login.php
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

	mux := http.NewServeMux()
	mux.HandleFunc(
		callback.Path, func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				stopChannel <- struct{}{}
			}()
			ctx := logr.NewContext(req.Context(), logr.FromContextOrDiscard(r.ctx))

			code := req.FormValue("code")
			state := req.FormValue("state")
			stateID, err := uuid.Parse(state)
			if err != nil {
				// we have no state for this request, discard it
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			pkce, err := r.store.FindPKCE(ctx, stateID)
			if err != nil {
				// we have no state for this request, discard it
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			profile, err := pkce.GetProfile(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			err = pkce.Destroy(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// check if more than 5 mins passed
			if time.Since(pkce.Time()) > 5*time.Minute {
				http.Error(w, "timeout ", http.StatusInternalServerError)
				return
			}

			token, err := r.oAuth2().Exchange(
				ctx,
				code,
				oauth2.SetAuthURLParam("code_verifier", pkce.GetCodeVerifier()),
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jt, err := r.verify(ctx, token.AccessToken)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			claims, err := newCharacterClaims(jt)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, err = profile.CreateCharacter(ctx, claims, token, pkce.GetReferenceData())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = r.store.CleanPKCE(ctx)
			_ = json.NewEncoder(w).Encode(jt)
		},
	)

	srv := &http.Server{Handler: mux}

	if callback.Port() == "" && callback.Scheme != "http" && r.AppConfig().Autocert {
		manager := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(callback.Hostname()),
			Cache:      autocert.DirCache(r.AppConfig().AutocertCache),
		}
		srv.TLSConfig = manager.TLSConfig()
	}

	go func() {
		var serveErr error
		switch {
		case callback.Port() == "" && callback.Scheme == "http":
			srv.Addr = ":80"
			serveErr = srv.ListenAndServe()
		case callback.Port() == "":
			srv.Addr = ":443"
			if r.AppConfig().Autocert {
				serveErr = srv.ListenAndServeTLS("", "")
			} else {
				serveErr = srv.ListenAndServeTLS(r.AppConfig().TLSCert, r.AppConfig().TLSKey)
			}
		default:
			srv.Addr = fmt.Sprintf(":%s", callback.Port())
			serveErr = srv.ListenAndServe()
		}
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		errChannel <- serveErr
	}()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()

	select {
	case serveErr := <-errChannel:
		return serveErr
	case <-stopChannel:
		return srv.Shutdown(ctx)
	case <-ctx.Done():
		return srv.Shutdown(ctx)
	}
}
