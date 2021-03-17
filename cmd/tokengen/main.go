package main

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ferocious-space/evesso/datastore"
	"github.com/ferocious-space/evesso/internal/tokengen"

	"github.com/ferocious-space/evesso/auth"
)

func main() {
	cfg := auth.AutoConfig("config.json")
	ts := cfg.TokenSource(datastore.NewDataStore(datastore.NewMemoryAccountStore()), "Ferocious Bite", "publicData")
	if !ts.Valid() {
		tk, err := tokengen.NewAuthenticator(cfg, "publicData").WebAuth("Ferocious Bite")
		if err != nil {
			logrus.WithError(err).Fatal("webauth")
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
			logrus.WithError(err).Fatal()
		}
		logrus.Infoln(tk)
	}
}
