package evesso

//go:generate go run ./internal/gen/scopes

const ISSUER = "login.eveonline.com"
const ISSUER_URL = "https://login.eveonline.com"
const AUDIENCE = "EVE Online"
const AUTOCONFIG_URL = "/.well-known/oauth-authorization-server"

var VALID_ISSUERS = []string{ISSUER, ISSUER_URL}
