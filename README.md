# evesso

EVE Online SSO authentication and token storage for Go.

It runs the OAuth2 authorization-code flow against EVE SSO, verifies the returned access token against the SSO JWKS,
persists characters and refresh tokens in PostgreSQL, and hands you a token source that plugs directly into the
generated ESI client in [eveapi](https://github.com/ferocious-space/eveapi).

```go
source, _ := sso.CharacterSource(character)
client, _ := esi.NewClientWithResponses(
esi.DefaultServer,
esi.WithRequestEditorFn(source.RequestEditor), // <- the integration point
)
```

Token refresh, JWKS rotation and re-persisting the rotated refresh token all happen behind `source.RequestEditor`;
callers do not manage token lifetime.

## Requirements

- Go 1.25
- PostgreSQL 13 or newer — the schema uses the built-in `gen_random_uuid()`
- An application registered at <https://developers.eveonline.com> with a client ID, secret and callback URL

## Install

```
go get github.com/ferocious-space/evesso
```

## Configuration

`AutoConfig` reads a YAML or JSON file, chosen by extension:

```yaml
# config.yaml
key: <CLIENT_ID>        # from developers.eveonline.com
secret: <CLIENT_SECRET>
callback: http://localhost:42000/sso/callback   # must match the application exactly
redirect: http://localhost:42000/done           # where ServeHTTP sends the browser afterwards
dsn: postgres://user:pass@localhost:5432/eve?sslmode=disable
autocert: false                                 # Let's Encrypt, only for a public callback on :443
autocertcache: /var/www/.cache
tlscert: /var/www/example.com.pem              # static TLS, used when autocert is false
tlskey: /var/www/example.com_key.pem
```

Only `key`, `secret`, `callback` and `dsn` are needed for a localhost flow. Keep this file out of version control —
`config.yaml` is already gitignored.

## Data model

| Type        | What it is                                                                                                                   |
|-------------|------------------------------------------------------------------------------------------------------------------------------|
| `Profile`   | A named bucket of characters — one per user, per bot, per tenant, whatever suits you. Carries arbitrary JSON in `GetData()`. |
| `Character` | One authenticated character: name, ID, owner hash, granted scopes, refresh token. Belongs to exactly one profile.            |
| `PKCE`      | A short-lived row holding the code verifier and state for an in-flight authorization. Valid for 5 minutes.                   |

A character is keyed by `(profile, character_id, character_name, owner, scopes)`. Authorizing the same character again
with a *different* scope set produces a second row rather than replacing the first, so a profile can hold several grants
for one character.

The `evesso` schema, its tables and a `sso_migrations` bookkeeping table are created automatically on first connect.

## Quick start

A desktop or CLI flow, where the library opens a browser and serves the callback itself. This is the whole thing end to
end:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/pkg/datastore/evessopg"
)

func main() {
	ctx := context.Background()

	// Reads config.yaml, connects, migrates, fetches the SSO metadata and JWKS.
	sso, err := evesso.AutoConfig(ctx, "./config.yaml", &evessopg.PGStore{}, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Profiles are found by name; create one the first time.
	profile, err := sso.Store().FindProfile(ctx, "default")
	if err != nil {
		if profile, err = sso.Store().NewProfile(ctx, "default", nil); err != nil {
			log.Fatal(err)
		}
	}

	scopes := []string{"esi-wallet.read_character_wallet.v1", "esi-skills.read_skills.v1"}
	source, err := sso.TokenSource(profile.GetID(), "Ferocious Bite", scopes...)
	if err != nil {
		log.Fatal(err)
	}

	// Valid() reports whether a stored character can produce a usable token.
	// If not, send the user through SSO once.
	if !source.Valid() {
		authURL, err := source.AuthURL(nil) // nil = no reference data
		if err != nil {
			log.Fatal(err)
		}
		// Opens the browser, serves the callback, blocks until done or 5 minutes pass.
		if err := sso.LocalhostAuth(authURL); err != nil {
			log.Fatal(err)
		}
	}

	token, err := source.Token()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("token valid until", token.Expiry)
}
```

`AuthURL` accepts arbitrary reference data, stored as JSON alongside the PKCE row and copied onto the character when
authorization completes. Use it to correlate a login with your own user record:

```go
authURL, err := source.AuthURL(map[string]any{"discord_id": "1234567890"})
```

Read it back later with `character.GetReferenceData()`.

## Hooking it to eveapi

`RequestEditor` matches `esi.RequestEditorFn`, so it goes straight into
`NewClient` or `NewClientWithResponses` and authenticates every request the client makes:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ferocious-space/eveapi/esi"
	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/pkg/datastore/evessopg"
)

func main() {
	ctx := context.Background()

	sso, err := evesso.AutoConfig(ctx, "./config.yaml", &evessopg.PGStore{}, nil)
	if err != nil {
		log.Fatal(err)
	}

	profile, err := sso.Store().FindProfile(ctx, "default")
	if err != nil {
		log.Fatal(err)
	}
	character, err := profile.FindCharacter(ctx, 0, "Ferocious Bite", "",
		[]string{"esi-wallet.read_character_wallet.v1"})
	if err != nil {
		log.Fatal(err)
	}

	source, err := sso.CharacterSource(character)
	if err != nil {
		log.Fatal(err)
	}

	client, err := esi.NewClientWithResponses(
		esi.DefaultServer,
		esi.WithCompatibilityDate(esi.CompatibilityDate),
		esi.WithRequestEditorFn(source.RequestEditor),
	)
	if err != nil {
		log.Fatal(err)
	}

	// esi.CharacterID is int64; evesso stores the ID as int32.
	wallet, err := client.GetCharactersCharacterIdWalletWithResponse(
		ctx, int64(character.GetCharacterID()), nil)
	if err != nil {
		log.Fatal(err)
	}
	if wallet.JSON200 == nil {
		log.Fatalf("ESI returned %s", wallet.HTTPResponse.Status)
	}
	fmt.Printf("balance: %.2f ISK\n", *wallet.JSON200)
}
```

`CharacterSource` derives the scope set from the character's stored grant, so the refreshed token carries exactly the
scopes that character was authorized with. Use `TokenSource` instead when you want to *request* a specific scope set and
have `Valid()` tell you whether a character already satisfies it.

### Iterating every stored character

`AllCharacters` plus `CharacterSource` gives you one authenticated client per character:

```go
characters, err := profile.AllCharacters(ctx)
if err != nil {
return err
}
for _, character := range characters {
source, err := sso.CharacterSource(character)
if err != nil {
return err
}
if !source.Valid() {
// Refresh token was revoked or expired; the character is marked inactive.
log.Printf("%s needs re-authorization", character.GetCharacterName())
continue
}
client, err := esi.NewClientWithResponses(
esi.DefaultServer,
esi.WithRequestEditorFn(source.RequestEditor),
)
if err != nil {
return err
}
_ = client
}
```

## Mains, alts, and identifying a returning user

A profile is an unordered bucket of characters. There is no built-in notion of a main, and nothing links characters
automatically — ESI deliberately does not expose which characters share an EVE account, so grouping only happens because
a user tells you.

Three mechanics do all the work:

| Question                                     | Mechanism                                                                                                                                                                              |
|----------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Which profile does an authorization land in? | The PKCE row. `profile.CreatePKCE(...)` stamps `profile_ref`, and the callback puts the character in *that* profile. The profile is chosen when you build the URL, not by who logs in. |
| How do I attach an alt?                      | Build another auth URL from the **same profile**. Whoever authenticates through it joins that profile.                                                                                 |
| How do I recognise a returning user?         | `sso.Store().FindCharacter(ctx, id, name, owner)` returns `(Profile, Character, error)` — any known character resolves to its profile.                                                 |

So yes: logging in with *any* character of a profile identifies the profile.

### Naming a profile after its first character

There is a bootstrap problem — a PKCE row needs a profile, but the character's name is not known until the callback
finishes. Create the profile with a placeholder, then rename it once the first character lands:

```go
const pendingPrefix = "pending:"

// First-time login. No character is known yet.
func StartLogin(ctx context.Context, sso *evesso.EVESSO, discordID string, scopes ...string) (string, error) {
profile, err := sso.Store().NewProfile(ctx, pendingPrefix+uuidLike(), nil)
if err != nil {
return "", err
}
return authURL(ctx, sso, profile, discordID, RoleMain, scopes...)
}

// Runs after the callback has persisted the character.
func FinishLogin(ctx context.Context, profile evesso.Profile) error {
if !isPending(profile.GetName()) {
return nil
}
characters, err := profile.AllCharacters(ctx)
if err != nil {
return err
}
if len(characters) == 0 {
return nil // authorization never completed
}
return profile.Rename(ctx, characters[0].GetCharacterName())
}
```

Placeholder names must be unique, since `profile_name` carries a unique index — that same index makes `Rename` fail if
the character name is already taken by another profile, which is what you want when someone tries to register a
character that already belongs elsewhere.

### Attaching alts

Identical to the first login except the profile already exists and the role differs:

```go
func AddAlt(ctx context.Context, sso *evesso.EVESSO, profile evesso.Profile,
discordID string, scopes ...string) (string, error) {
return authURL(ctx, sso, profile, discordID, RoleAlt, scopes...)
}

func authURL(ctx context.Context, sso *evesso.EVESSO, profile evesso.Profile,
discordID, role string, scopes ...string) (string, error) {
pkce, err := profile.CreatePKCE(ctx, CharacterMeta{
DiscordID: discordID,
Role:      role,
AddedAt:   time.Now().UTC().Format(time.RFC3339),
}, scopes...)
if err != nil {
return "", err
}
return sso.AuthUrl(pkce), nil
}
```

### Logging in with any character

```go
profile, character, err := sso.Store().FindCharacter(ctx, 0, "Ferocious Bite", "")
```

Pass `0` / `""` for the fields you are not matching on. This only finds **active**
characters — one whose refresh token was revoked is marked inactive and will not resolve, so treat "not found" as "needs
re-authorization" rather than "unknown user".

### Attaching your own identity, e.g. a Discord ID

Per-character JSON travels with the authorization: pass it to `CreatePKCE`, and it lands on the character.

```go
type CharacterMeta struct {
DiscordID string `json:"discord_id"` // string, not a number — see below
Role      string `json:"role"`       // "main" or "alt"
AddedAt   string `json:"added_at"`
}
```

Reading it back needs a re-marshal, because `GetReferenceData()` returns decoded JSON as `interface{}` rather than your
struct:

```go
func MetaOf(character evesso.Character) (CharacterMeta, error) {
var meta CharacterMeta
raw := character.GetReferenceData()
if raw == nil {
return meta, nil
}
buf, err := json.Marshal(raw)
if err != nil {
return meta, err
}
return meta, json.Unmarshal(buf, &meta)
}
```

Splitting a profile into its main and alts is then a filter over
`AllCharacters`:

```go
func Roster(ctx context.Context, profile evesso.Profile) (evesso.Character, []evesso.Character, error) {
characters, err := profile.AllCharacters(ctx)
if err != nil {
return nil, nil, err
}
var main evesso.Character
var alts []evesso.Character
for _, character := range characters {
meta, err := MetaOf(character)
if err != nil {
return nil, nil, err
}
if meta.Role == RoleMain && main == nil {
main = character
continue
}
alts = append(alts, character)
}
if main == nil {
return nil, alts, ErrNoMain
}
return main, alts, nil
}
```

### Constraints to design around

- **Store 64-bit IDs as strings.** Reference data round-trips through
  `interface{}`, so a JSON number becomes a `float64`. A Discord snowflake exceeds its exact range and corrupts
  silently — `123456789012345678` comes back as `123456789012345680`.
- **Reference data is write-once.** `Character` has no update method for it, so a character's role is fixed when it is
  authorized. To change a main, delete the character and re-authorize it, or keep roles in your own database keyed by
  `character.GetID()`.
- **Re-authorizing does not refresh reference data.** The upsert only writes
  `refresh_token`, so an existing character keeps its original role and timestamp.
- **`reference_data` is not indexed.** Filter it in Go within a profile; use profile names and `FindCharacter` for
  lookups that need to be fast.
- **A profile can hold the same character more than once** if it was authorized with different scope sets — the identity
  constraint includes `scopes`.

## Serving the callback yourself

For a web application, `*EVESSO` is an `http.Handler`. Mount it at the path your callback URL points to; it exchanges
the code, verifies the token, persists the character and redirects to the `redirect` from your config:

```go
mux := http.NewServeMux()
mux.Handle("/sso/callback", sso)
log.Fatal(http.ListenAndServe(":42000", mux))
```

Build the authorization URL per user. Create the PKCE row from the profile, then render the link:

```go
pkce, err := profile.CreatePKCE(ctx, map[string]any{"user_id": 42},
"esi-wallet.read_character_wallet.v1")
if err != nil {
return err
}
authURL := sso.AuthUrl(pkce)
```

PKCE rows expire after 5 minutes. Call `sso.Store().CleanPKCE(ctx)` periodically to drop abandoned ones.

## Scopes

`ALL_SCOPES` holds every scope the pinned ESI spec defines. It is generated, not hand-maintained:

```go
source, err := sso.TokenSource(profile.GetID(), "Ferocious Bite", evesso.ALL_SCOPES...)
```

Requesting everything is convenient for a personal tool and poor practice for anything a third party logs into — ask for
the scopes you use.

To pick up scopes added by a newer ESI compatibility date, regenerate from a checkout of eveapi next to this one:

```
go generate ./...
```

That runs `internal/gen/scopes` against `../eveapi/openapi.json` and rewrites
`scopes_gen.go`. Point it elsewhere with `-spec`:

```
go run ./internal/gen/scopes -spec /path/to/openapi.json
```

## How verification works

Identity is never taken from an unverified token. `CharacterClaims` — the name, character ID, owner hash and scopes a
character row is built from — has unexported fields and can only be constructed from a `jwt.Token` whose signature,
issuer and audience have been checked against the SSO JWKS. That makes it impossible for a `DataStore` implementation to
be handed an identity nobody verified, including through the exported `Save` method.

Accepted issuers are both `login.eveonline.com` and `https://login.eveonline.com`, since the published metadata uses the
scheme-prefixed form while older tokens do not. Clock skew of 30 seconds is tolerated.

## Implementing your own DataStore

`evessopg` is one implementation of the `DataStore`, `Profile`, `Character` and
`PKCE` interfaces in `DataStore.go`; nothing ties the library to PostgreSQL.
`CreateCharacter` receives already-verified `CharacterClaims` and must persist them as given — it must not re-parse
`token.AccessToken` to derive identity.

## Things worth knowing

- **`TokenSource` and `CharacterSource` return an unexported type.** You can call its methods, but you cannot name it in
  a struct field or function signature. Declare your own interface if you need to store one:

  ```go
  type TokenSource interface {
      Token() (*oauth2.Token, error)
      Valid() bool
      AuthURL(referenceData interface{}) (string, error)
      RequestEditor(ctx context.Context, req *http.Request) error
  }
  ```

- **`AutoConfig` reaches the network.** It fetches the SSO metadata document and performs a blocking JWKS fetch, so it
  fails if `login.eveonline.com` is unreachable at startup.
- **Refresh and access tokens are stored in plaintext.** Protect the database accordingly; anyone with read access to
  `evesso.characters` can impersonate every character in it.
- **A revoked refresh token marks the character inactive** rather than deleting it. `Valid()` returns false and
  `FindCharacter` skips it until it is re-authorized.
- **`LocalhostAuth` blocks** for up to 5 minutes waiting for the callback, and needs a browser — it is for CLI and
  desktop use, not servers.

## License

MIT. See [LICENSE](LICENSE).
