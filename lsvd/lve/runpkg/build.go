package runpkg

import (
	"archive/zip"
	"bytes"
	"io"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type ConfigFile struct {
	Name         string `yaml:"name"`
	TemplateFile string `yaml:"template_file"`
	Data         any    `yaml:"config"`
}

type Config struct {
	Name string   `yaml:"name"`
	Tags []string `yaml:"tags"`

	Configs []ConfigFile `yaml:"config"`
}

func ParseConfig(r io.Reader) (*Config, error) {
	var cfg Config
	err := yaml.NewDecoder(r).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

type RunPkg struct {
	data []byte
}

func Build(log *slog.Logger, path string) (*RunPkg, error) {
	dir := os.DirFS(path)

	f, err := dir.Open("config.yml")
	if err != nil {
		return nil, err
	}

	defer f.Close()

	cfg, err := ParseConfig(f)
	if err != nil {
		return nil, err
	}

	log.Debug("building runpkg", "name", cfg.Name)

	var b bytes.Buffer

	zw := zip.NewWriter(&b)

	err = zw.AddFS(os.DirFS(path))
	if err != nil {
		return nil, err
	}

	rp := &RunPkg{
		data: b.Bytes(),
	}

	return rp, err
}
