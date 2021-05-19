package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
	"github.com/gobuffalo/pop/v5"

	"github.com/ferocious-space/evesso"
	"github.com/ferocious-space/evesso/internal/utils"
)

func main() {
	pop.Color = false
	fmt.Println(pop.AvailableDialects)
	logger := stdr.NewWithOptions(log.New(os.Stderr, " ", log.LstdFlags), stdr.Options{LogCaller: stdr.All, Depth: 1})
	newContext := logr.NewContext(context.Background(), logger)
	config, err := evesso.AutoConfig(newContext, "./config.yaml", nil)
	if err != nil {
		log.Fatalln(err)
		return
	}
	defaultProfile, err := config.Store().FindProfile(context.TODO(), "default")
	if err != nil {
		defaultProfile, err = config.Store().NewProfile(context.TODO(), "default", nil)
		if err != nil {
			log.Fatalln(err)
		}
	}
	source, err := config.TokenSource(defaultProfile, "Ferocious Bite", evesso.ALL_SCOPES...)
	if err != nil {
		log.Fatalln(err)
		return
	}
	if !source.Valid() {
		au, err := source.AuthUrl()
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
	fmt.Println(source.Valid())
}
