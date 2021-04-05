package main

import (
	"context"
	"log"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/ferocious-space/durableclient"
	"github.com/ferocious-space/eveapi"
	"github.com/ferocious-space/eveapi/esi/character"
	"github.com/ferocious-space/httpcache/BoltCache"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/pkg/authenticator"

	"github.com/ferocious-space/evesso/auth"
)

func main() {
	logger, err := zap.NewDevelopment()
	defer logger.Sync()
	if err != nil {
		return
	}
	opt := bbolt.DefaultOptions
	opt.FreelistType = bbolt.FreelistMapType
	db, err := bbolt.Open("cache.db", os.ModePerm, opt)
	if err != nil {
		logger.Fatal("", zap.Error(err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := auth.AutoConfig(ctx, "config.yaml", durableclient.NewClient("evesso", logger.Named("autoconfig")))
	if err != nil {
		log.Fatal("unable to autoconfig:", err.Error())
	}
	ts := cfg.TokenSource(ctx, datastore.NewDataStore(datastore.NewBoltAccountStore(db)), "Ferocious Bite", auth.ALL_SCOPES)
	if !ts.Valid() {

		tk, err := authenticator.NewAuthenticator(cfg, auth.ALL_SCOPES).WebAuth("Ferocious Bite")
		if err != nil {
			log.Fatal("webauth:", err.Error())
		}
		ts.Save(tk)
		if !ts.Valid() {
			panic(tk)
		}
	}
	apic := eveapi.NewAPIClient(durableclient.NewCachedClient("eveapi", BoltCache.NewBoltCache(db, "ESI", logger), logger.Named("ESI")))
	roles, err := apic.Character.GetCharactersCharacterIDRoles(character.NewGetCharactersCharacterIDRolesParams().WithCharacterID(ts.CharacterId), ts)
	if err != nil {
		logger.Fatal(err.Error(), zap.Error(err))
	}
	spew.Dump(roles)

}
