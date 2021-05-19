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
	Key string `json:"key"`
	//ESI API Secret
	Secret string `json:"secret"`
	//Callback
	Callback string `json:"callback"`
	//Redirect to URL after successful completion of authentication
	Redirect string `json:"redirect"`
	//DSN database connection string
	DSN string `json:"dsn" yaml:"dsn"`
	//Autocert enable/disable letsencrypt
	Autocert bool `json:"autocert"`
	//AutocertCache location to save certs if letsencrypt is enabled
	AutocertCache string `json:"autocertcache"`
	//TLSCert path to pem Cert file to use for https if letsencrypt is disabled
	TLSCert string `json:"tlscert"`
	//TLSKey path to pem Key file to use for https if letsencrypt is disabled
	TLSKey string `json:"tlskey"`
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
