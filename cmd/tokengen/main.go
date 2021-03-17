package main

import (
	"context"
	"log"

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
	ts := cfg.TokenSource(ctx, datastore.NewDataStore(datastore.NewMemoryAccountStore()), "Ferocious Bite", []string{"publicData"})
	if !ts.Valid() {
		tk, err := tokengen.NewAuthenticator(cfg, []string{"publicData"}).WebAuth("Ferocious Bite")
		if err != nil {
			log.Fatal("webauth:", err.Error())
		}
		ts.Save(tk)
		if !ts.Valid() {
			panic(tk)
		}
	}
}
