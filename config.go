package evesso

import (
	"os"
	"path/filepath"

	"github.com/goccy/go-json"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type appConfig struct {
	//ESI API Key
	Key string
	//ESI API Secret
	Secret string
	//Callback
	Callback string
}

func (c *appConfig) Load(path string) error {
	cfg, err := os.Open(path)
	if err != nil {
		return err
	}
	defer cfg.Close()
	switch filepath.Ext(path) {
	case ".yaml", ".yml":
		if err = yaml.NewDecoder(cfg).Decode(&c); err != nil {
			return err
		}
	case ".json":
		if err = json.NewDecoder(cfg).Decode(&c); err != nil {
			return err
		}
	default:
		return errors.New("unknown configuration file")
	}
	return nil
}
