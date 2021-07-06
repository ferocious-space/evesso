package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/internal/utils"
	"github.com/ferocious-space/evesso/pkg/datastore/postgres"
)

func main() {
	newContext := logr.NewContext(context.Background(), stdr.New(log.New(os.Stdout, "", 0)))
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
	if !source.Valid() {
		au, err := source.AuthUrl(236432573567548)
		if err != nil {
			log.Fatalln(err)
			return
		}
		err = utils.OSExec(au)
		if err != nil {
			log.Fatalln(err)
			return
		}
		err = config.LocalhostAuth(au)
		if err != nil {
			log.Fatalln(err)
			return
		}
	}
	profiles, err := config.Store().AllProfiles(context.Background())
	if err != nil {
		return
	}
	for _, p := range profiles {
		characters, err := p.AllCharacters(context.Background())
		if err != nil {
			return
		}
		for _, c := range characters {
			characterSource, err := config.CharacterSource(c)
			if err != nil {
				return
			}
			err = c.UpdateActiveState(context.Background(), characterSource.Valid())
			if err != nil {
				return
			}
		}
	}
	fmt.Println(source.Valid())
}
