package auth

import (
	"net/http"

	"github.com/ferocious-space/durableclient"
	"go.uber.org/zap"
)

var CONST_ISSUER = "login.eveonline.com"
var CONST_AUTOCONFIG_URL = "/.well-known/oauth-authorization-server"
var ssoClient *http.Client

func init() {
	ssoClient = durableclient.NewClient("JWKS", "github.com/ferocious-space/evesso", zap.L())
}
