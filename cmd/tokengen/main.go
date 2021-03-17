package main

import (
	"log"
	"time"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/internal/tokengen"

	"github.com/ferocious-space/evesso/auth"
)

func main() {
	cfg, err := auth.AutoConfig("config.json")
	if err != nil {
		log.Fatal("unable to autoconfig:", err.Error())
	}
	ts := cfg.TokenSource(datastore.NewDataStore(datastore.NewMemoryAccountStore()), "Ferocious Bite", "publicData")
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
