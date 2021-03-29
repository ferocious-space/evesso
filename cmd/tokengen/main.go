package main

import (
	"context"
	"log"

	"github.com/davecgh/go-spew/spew"
	"github.com/ferocious-space/durableclient"
	"github.com/ferocious-space/eveapi"
	"github.com/ferocious-space/eveapi/esi/character"
	"github.com/ferocious-space/httpcache"
	"go.uber.org/zap"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/internal/tokengen"

	"github.com/ferocious-space/evesso/auth"
)

func main() {
	logger, err := zap.NewDevelopment()
	defer logger.Sync()
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := auth.AutoConfig(ctx, "config.yaml", durableclient.NewClient("evesso", logger.Named("autoconfig")))
	if err != nil {
		log.Fatal("unable to autoconfig:", err.Error())
	}
	ts := cfg.TokenSource(ctx, datastore.NewDataStore(datastore.NewMemoryAccountStore()), "Ferocious Bite", auth.ALL_SCOPES)
	if !ts.Valid() {
		tk, err := tokengen.NewAuthenticator(cfg, auth.ALL_SCOPES).WebAuth("Ferocious Bite")
		if err != nil {
			log.Fatal("webauth:", err.Error())
		}
		ts.Save(tk)
		if !ts.Valid() {
			panic(tk)
		}
	}
	apic := eveapi.NewAPIClient(durableclient.NewCachedClient("eveapi", httpcache.NewLRUCache(1<<20*256, 300), logger.Named("ESI")))
	roles, err := apic.Character.GetCharactersCharacterIDRoles(character.NewGetCharactersCharacterIDRolesParams().WithCharacterID(ts.CharacterId), ts)

	if err != nil {
		logger.Fatal(err.Error(), zap.Error(err))
	}
	spew.Dump(roles)
}
