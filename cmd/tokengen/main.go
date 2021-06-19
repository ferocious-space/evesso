package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-logr/logr"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/pkg/datastore/postgres"
)

func main() {
	newContext := logr.NewContext(context.Background(), logr.Discard())
	config, err := evesso.AutoConfig(newContext, "./config.yaml", &postgres.PGStore{}, nil)
	if err != nil {
		log.Fatalln(err)
		return
	}
	defaultProfile, err := config.Store().FindProfile(newContext, "default")
	if err != nil {
		defaultProfile, err = config.Store().NewProfile(newContext, "default")
		if err != nil {
			log.Fatalln(err)
		}
	}
	err = config.Store().CleanPKCE(newContext)
	if err != nil {
		log.Fatalln(err)
		return
	}
	source, err := config.TokenSource(defaultProfile.GetID(), "Ferocious Bite", evesso.ALL_SCOPES...)
	if err != nil {
		log.Fatalln(err)
		return
	}
	//if !source.Valid() {
	//	au, err := source.AuthUrl()
	//	if err != nil {
	//		log.Fatalln(err)
	//		return
	//	}
	//	err = utils.OSExec(au)
	//	if err != nil {
	//		log.Fatalln(err)
	//		return
	//	}
	//	err = config.LocalhostAuth(au)
	//	if err != nil {
	//		log.Fatalln(err)
	//		return
	//	}
	//}
	fmt.Println(source.Valid())
}
