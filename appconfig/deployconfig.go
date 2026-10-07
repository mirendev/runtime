package appconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

const DeployConfigPath = ".miren/deploy.toml"

type DeployTarget struct {
	Name      string `toml:"name"`
	Cluster   string `toml:"cluster"`
	ClusterID string `toml:"cluster_id,omitempty"`
}

type DeployConfig struct {
	Targets []DeployTarget `toml:"targets"`
}

func LoadDeployConfigUnder(dir string) (*DeployConfig, error) {
	path := filepath.Join(dir, DeployConfigPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dc DeployConfig
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dc); err != nil {
		return nil, enrichDecodeError(path, data, err)
	}
	if err := dc.Validate(); err != nil {
		return nil, enrichValidationError(path, data, err)
	}
	return &dc, nil
}

func SaveDeployConfigUnder(dir string, dc *DeployConfig) error {
	if err := dc.Validate(); err != nil {
		return err
	}

	data, err := toml.Marshal(dc)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, DeployConfigPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func RemoveDeployConfigUnder(dir string) error {
	err := os.Remove(filepath.Join(dir, DeployConfigPath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (dc *DeployConfig) Validate() error {
	if len(dc.Targets) == 0 {
		return &ValidationError{KeyPath: "targets", Message: "at least one deploy target is required"}
	}

	seen := make(map[string]bool, len(dc.Targets))
	for i, target := range dc.Targets {
		prefix := fmt.Sprintf("targets[%d]", i)
		if target.Name == "" {
			return &ValidationError{KeyPath: prefix + ".name", Message: fmt.Sprintf("%s: name is required", prefix)}
		}
		if seen[target.Name] {
			return &ValidationError{KeyPath: prefix + ".name", Message: fmt.Sprintf("%s: duplicate name %q", prefix, target.Name)}
		}
		seen[target.Name] = true
		if target.Cluster == "" {
			return &ValidationError{KeyPath: prefix + ".cluster", Message: fmt.Sprintf("%s: cluster is required", prefix)}
		}
	}
	return nil
}

// Target resolves a named target, or the first target when name is empty.
func (dc *DeployConfig) Target(name string) (*DeployTarget, error) {
	if name == "" {
		return &dc.Targets[0], nil
	}
	for i := range dc.Targets {
		if dc.Targets[i].Name == name {
			return &dc.Targets[i], nil
		}
	}
	return nil, fmt.Errorf("deploy target %q not found", name)
}

func (dc *DeployConfig) AddTarget(target DeployTarget, makeDefault bool) error {
	for i := range dc.Targets {
		if dc.Targets[i].Name == target.Name {
			return fmt.Errorf("deploy target %q already exists", target.Name)
		}
	}
	if makeDefault {
		dc.Targets = append([]DeployTarget{target}, dc.Targets...)
	} else {
		dc.Targets = append(dc.Targets, target)
	}
	return nil
}

func (dc *DeployConfig) RemoveTarget(name string) error {
	for i := range dc.Targets {
		if dc.Targets[i].Name == name {
			dc.Targets = append(dc.Targets[:i], dc.Targets[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("deploy target %q not found", name)
}

func (dc *DeployConfig) SetDefaultTarget(name string) error {
	for i := range dc.Targets {
		if dc.Targets[i].Name == name {
			target := dc.Targets[i]
			copy(dc.Targets[1:i+1], dc.Targets[:i])
			dc.Targets[0] = target
			return nil
		}
	}
	return fmt.Errorf("deploy target %q not found", name)
}
