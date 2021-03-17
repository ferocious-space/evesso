package auth

import (
	"crypto/rsa"
	"encoding/json"
	"os"

	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

type Config struct {
	Key           string
	Secret        string
	Callback      string
	PublicKey     *rsa.PublicKey
	CharacterName string
	Scopes        []string
}

func (c *Config) Load(path string, logger *zap.Logger) error {
	cfg, err := os.Open(path)
	if err != nil {
		logrus.WithError(err).Fatal("config.json")
	}
	if err = json.NewDecoder(cfg).Decode(&c); err != nil {
		logrus.WithError(err).Fatal("config.json")
	}
	return nil
}
