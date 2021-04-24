package authenticator

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/lestrrat-go/jwx/jwt"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/ferocious-space/evesso/auth"
	"github.com/ferocious-space/evesso/internal/utils"
)

var ErrAuthStateMissing = errors.New("auth state is not setup")
var ErrInvalidState = errors.New("states do not match")
var ErrSomethingWrong = errors.New("something went wrong")

type authenticator struct {
	cfg  *oauth2.Config
	acfg *auth.OAuthAutoConfig
	pkce *pkce
}

func NewAuthenticator(acfg *auth.OAuthAutoConfig, scopes []string) *authenticator {
	return &authenticator{cfg: acfg.Oauth2Config(scopes...), acfg: acfg, pkce: nil}
}

func (r *authenticator) authURL(CharacterName string) string {
	r.pkce = makePKCE(CharacterName)
	return r.cfg.AuthCodeURL(
		r.pkce.state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challange", r.pkce.codeChallange),
		oauth2.SetAuthURLParam("code_challange_method", r.pkce.codeChallangeMethod),
	)
}

func (r *authenticator) validState(state string) error {
	if r.pkce.state != state {
		return ErrInvalidState
	}
	return nil
}

func (r *authenticator) exchangeCode(ctx context.Context, state string, code string) (*oauth2.Token, error) {
	if r.pkce == nil {
		return nil, ErrAuthStateMissing
	}
	if err := r.validState(state); err != nil {
		return nil, err
	}
	token, err := r.cfg.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", r.pkce.codeVerifier),
	)
	defer func() { r.pkce = nil }()
	if jwtToken, err := r.acfg.JWT(ctx, token); err != nil {
		return nil, err
	} else {
		verifier, err := base64.RawURLEncoding.DecodeString(r.pkce.codeVerifier)
		if err != nil {
			return nil, err
		}
		aes32, err := aes.NewCipher(verifier)
		if err != nil {
			return nil, err
		}

		nonce := verifier[:12]
		aesgcm, err := cipher.NewGCM(aes32)
		if err != nil {
			return nil, err
		}
		binState, err := base64.RawURLEncoding.DecodeString(state)
		if err != nil {
			return nil, err
		}

		binName, err := aesgcm.Open(nil, nonce, binState, nil)
		if err != nil {
			return nil, err
		}
		if err := jwt.Validate(jwtToken, jwt.WithIssuer(auth.CONST_ISSUER), jwt.WithClaimValue("name", string(binName))); err != nil {
			return nil, err
		}
	}
	return token, err
}

func (r *authenticator) WebAuth(CharacterName string, pub *rsa.PublicKey) (*oauth2.Token, error) {

	pks := pub.Size()
	if pks == 0 {
		return nil, errors.New("please provide valid public key")
	}

	if err := utils.OSExec(r.authURL(CharacterName)); err != nil {
		return nil, err
	}

	callback, err := url.Parse(r.acfg.AppConfig().Callback)

	if err != nil {
		return nil, err
	}

	stopChannel := make(chan struct{}, 1)

	outToken := new(oauth2.Token)
	router := mux.NewRouter()

	router.HandleFunc(
		callback.Path, func(writer http.ResponseWriter, request *http.Request) {
			code := request.FormValue("code")
			state := request.FormValue("state")
			encoder := json.NewEncoder(writer)
			token, err := r.exchangeCode(context.WithValue(request.Context(), oauth2.HTTPClient, r.acfg.SSOHttpClient), state, code)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				stopChannel <- struct{}{}
				_ = encoder.Encode(err.Error())
				return
			}
			encToken, err := EncryptWithPublicKey([]byte(token.RefreshToken), pub)
			if err != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				stopChannel <- struct{}{}
				_ = encoder.Encode(err.Error())
				return
			}
			data := base64.RawURLEncoding.EncodeToString(encToken)
			writer.Header().Set("Content-Disposition", "attachment; filename=token.json")
			writer.Header().Set("Content-Type", request.Header.Get("Content-Type"))
			_ = encoder.Encode(data)
			outToken = token
			stopChannel <- struct{}{}
		},
	)

	hs := &http.Server{Addr: fmt.Sprintf("%s:%s", callback.Hostname(), callback.Port()), Handler: router}

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()

	go func() {
		if err := hs.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal("internal server error:", err.Error())
		}
	}()

	select {
	case <-stopChannel:
		err = hs.Shutdown(ctx)
		if err != nil {
			log.Fatal("Error stopping webserver:", err.Error())
		}
	case <-ctx.Done():
		err = hs.Shutdown(ctx)
		if err != nil {
			log.Fatal("Error stopping webserver:", err.Error())
		}
	}

	return outToken, nil
}
