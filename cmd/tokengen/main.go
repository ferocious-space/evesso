package main

import (
	"context"
	"log"
	"time"

	"go.uber.org/zap"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/internal/tokengen"

	"github.com/ferocious-space/evesso/auth"
)

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := auth.AutoConfig(ctx, "config.json", logger.Named("AutoConfig"))
	if err != nil {
		log.Fatal("unable to autoconfig:", err.Error())
	}
	ts := cfg.TokenSource(ctx, datastore.NewDataStore(datastore.NewMemoryAccountStore()), "Ferocious Bite", "publicData")
	if !ts.Valid() {
		tk, err := tokengen.NewAuthenticator(cfg, "publicData").WebAuth("Ferocious Bite")
		if err != nil {
			log.Fatal("webauth:", err.Error())
		}
		ts.Save(tk)
		if !ts.Valid() {
			panic(tk)
		}
	}
	for {
		time.Sleep(1 * time.Minute)
		tk, err := ts.Token()
		if err != nil {
			log.Fatal(err.Error())
		}
		log.Println(tk)
	}
}
