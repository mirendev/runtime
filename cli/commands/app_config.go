package commands

import (
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

type AppConfig struct {
	Name string `toml:"name"`
}

const AppConfigPath = ".miren/app.toml"

func LoadAppConfig() (*AppConfig, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for dir != "/" {
		path := filepath.Join(dir, AppConfigPath)
		fi, err := os.Open(path)
		if err == nil {
			defer fi.Close()

			var ac AppConfig
			dec := toml.NewDecoder(fi)
			err = dec.Decode(&ac)
			if err != nil {
				return nil, err
			}

			return &ac, nil
		}

		dir = filepath.Dir(dir)
	}

	return nil, nil
}
