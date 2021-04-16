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
	"github.com/go-logr/zapr"
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

	cl := durableclient.NewDurableClient(zapr.NewLogger(logger.Named("httpClient")), "github.com/ferocious-space/durableclient")
	// cfg, err := auth.AutoConfig(ctx, "config.yaml", durableclient.NewClient("evesso", logger.Named("autoconfig")))
	cfg, err := auth.AutoConfig(ctx, "config.yaml", cl.Client(durableclient.OptionContext(ctx)))
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
	apic := eveapi.NewAPIClient(cl.Client(durableclient.OptionRetrier(), durableclient.OptionConnectionPooling(), durableclient.OptionContext(ctx), durableclient.OptionCache(BoltCache.NewBoltCache(db, "eve", logger))))
	roles, err := apic.Character.GetCharactersCharacterIDRoles(character.NewGetCharactersCharacterIDRolesParams().WithCharacterID(ts.CharacterId), ts)
	if err != nil {
		logger.Fatal(err.Error(), zap.Error(err))
	}
	corph, err := apic.Character.GetCharactersCharacterIDCorporationhistory(character.NewGetCharactersCharacterIDCorporationhistoryParams().WithCharacterID(ts.CharacterId))
	if err != nil {
		logger.Fatal(err.Error(), zap.Error(err))
	}
	spew.Dump(roles)
	spew.Dump(corph.GetPayload())

}
