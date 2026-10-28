package commands

import (
	"errors"
	"fmt"
	"os"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// pickCluster never lets an implicit global selection decide a deployment.
func pickCluster(names []string, useSole, interactive bool) (string, error) {
	if len(names) == 0 {
		return "", fmt.Errorf("no clusters configured; add one with 'miren cluster add': %w", ErrNoConfig)
	}
	if useSole && len(names) == 1 {
		return names[0], nil
	}
	if !interactive {
		return "", fmt.Errorf("cluster selection required; pass -C <cluster> or set MIREN_CLUSTER (choices: %v)", names)
	}
	items := make([]ui.PickerItem, len(names))
	for i, name := range names {
		items[i] = ui.SimplePickerItem{Text: name}
	}
	selected, err := ui.RunPicker(items, ui.WithTitle("Select a cluster:"), ui.WithHeaders([]string{"CLUSTER"}))
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", fmt.Errorf("cluster selection cancelled")
	}
	return selected.ID(), nil
}

func (a *AppCentric) selectAppCluster(targetName string, deploying bool) (string, error) {
	dc, err := appconfig.LoadDeployConfigUnder(a.ResolvedDir())
	if err != nil {
		return "", fmt.Errorf("error loading %s: %w", appconfig.DeployConfigPath, err)
	}
	if dc == nil {
		if targetName != "" {
			return "", fmt.Errorf("--target requires %s", appconfig.DeployConfigPath)
		}
		if a.Cluster != "" {
			return a.Cluster, nil
		}
		cfg, err := a.LoadConfig()
		if err != nil {
			if errors.Is(err, clientconfig.ErrNoConfig) || errors.Is(err, ErrNoConfig) {
				return "", fmt.Errorf("no clusters configured; add one with 'miren cluster add': %w", ErrNoConfig)
			}
			return "", fmt.Errorf("cannot select cluster; run 'miren cluster add': %w", err)
		}
		if !deploying && cfg.ActiveCluster() != "" {
			return cfg.ActiveCluster(), nil
		}
		return pickCluster(cfg.GetClusterNames(), deploying, a.isInteractive())
	}
	if targetName != "" {
		target, err := dc.Target(targetName)
		if err != nil {
			return "", err
		}
		return a.clusterNameForTarget(target)
	}
	if a.Cluster != "" {
		for _, target := range dc.Targets {
			if target.Name == a.Cluster {
				return a.clusterNameForTarget(&target)
			}
		}
		for _, target := range dc.Targets {
			if target.Cluster == a.Cluster {
				fmt.Fprintln(os.Stderr, "Warning: selecting a deploy target by cluster name is deprecated; use -C "+target.Name+" instead.")
				return a.clusterNameForTarget(&target)
			}
		}
	}
	var names []string
	for _, target := range dc.Targets {
		names = append(names, target.Name)
		if a.Cluster != "" {
			name, err := a.clusterNameForTarget(&target)
			if err == nil && a.Cluster == name {
				fmt.Fprintln(os.Stderr, "Warning: selecting a deploy target by cluster name is deprecated; use -C "+target.Name+" instead.")
				return name, nil
			}
		}
	}
	if a.Cluster != "" {
		return "", fmt.Errorf("cluster %q is not a deploy target in %s; choices: %v", a.Cluster, appconfig.DeployConfigPath, names)
	}
	// Count targets, not distinct clusters: target order is not a default.
	selected, err := pickCluster(names, true, a.isInteractive())
	if err != nil {
		return "", err
	}
	target, err := dc.Target(selected)
	if err != nil {
		return "", err
	}
	return a.clusterNameForTarget(target)
}

func clusterSelectionError(err error) error {
	summary := "cluster selection failed"
	if errors.Is(err, ErrNoConfig) {
		summary = "no clusters configured"
	}
	return &ui.Diagnostic{
		Summary: summary,
		Detail:  err.Error(),
		Cause:   err,
	}
}
