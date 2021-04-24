package auth

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Key      string
	Secret   string
	Callback string
}

func (c *Config) Load(path string) error {
	cfg, err := os.Open(path)
	if err != nil {
		return err
	}
	defer cfg.Close()
	if err = yaml.NewDecoder(cfg).Decode(&c); err != nil {
		return err
	}
	return nil
}
