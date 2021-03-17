package auth

import (
	"crypto/rsa"
	"encoding/json"
	"os"
)

type Config struct {
	Key           string
	Secret        string
	Callback      string
	PublicKey     *rsa.PublicKey
	CharacterName string
	Scopes        []string
}

func (c *Config) Load(path string) error {
	cfg, err := os.Open(path)
	if err != nil {
		return err
	}
	if err = json.NewDecoder(cfg).Decode(&c); err != nil {
		return err
	}
	return nil
}
