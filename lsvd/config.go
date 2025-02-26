package lsvd

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
)

type Config struct {
	CachePath string `hcl:"cache_path"`

	Storage struct {
		FilePath string `hcl:"file_path,optional"`
		S3       struct {
			Bucket    string `hcl:"bucket"`
			Region    string `hcl:"region"`
			AccessKey string `hcl:"access_key,optional"`
			SecretKey string `hcl:"secret_key,optional"`
			Directory string `hcl:"directory,optional"`
			URL       string `hcl:"host,optional"`
		} `hcl:"s3,block"`
	} `hcl:"storage,block"`
}

func LoadConfig(path string) (*Config, error) {
	var (
		ctx hcl.EvalContext
		cfg Config
	)

	err := hclsimple.DecodeFile(path, &ctx, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
