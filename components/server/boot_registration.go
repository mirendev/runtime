//go:build linux

package server

import (
	"context"
	"log/slog"
	"path/filepath"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/registration"
)

type registrationBootInputs struct {
	log      *slog.Logger
	dataPath string
}

type registrationBootOutput struct {
	cloudAuth coordinate.CloudAuthConfig
	anchor    string
}

type registrationBoot struct {
	component *boot.Component
	inputs    registrationBootInputs
	output    boot.Output[registrationBootOutput]
}

func registrationInputs(options StartOptions) registrationBootInputs {
	return registrationBootInputs{log: options.Log, dataPath: options.Config.Server.GetDataPath()}
}

func newRegistrationBoot(inputs registrationBootInputs) *registrationBoot {
	b := &registrationBoot{inputs: inputs}
	b.component, b.output = boot.Provide0("registration", b.start)
	return b
}

func (b *registrationBoot) start(context.Context) (registrationBootOutput, error) {
	dir := filepath.Join(b.inputs.dataPath, "server")
	reg, err := registration.LoadRegistration(dir)
	if err != nil {
		b.inputs.log.Warn("failed to load registration", "error", err, "dir", dir)
		return registrationBootOutput{}, nil
	}
	if reg == nil {
		b.inputs.log.Info("no cluster registration found")
		return registrationBootOutput{}, nil
	}
	if reg.Status == "pending" {
		b.inputs.log.Info("found pending cluster registration",
			"cluster-name", reg.ClusterName,
			"registration-id", reg.RegistrationID,
			"expires-at", reg.ExpiresAt)
		return registrationBootOutput{}, nil
	}
	if reg.Status != "approved" {
		b.inputs.log.Warn("cluster registration is not approved",
			"status", reg.Status,
			"cluster-name", reg.ClusterName)
		return registrationBootOutput{}, nil
	}

	b.inputs.log.Info("loaded cluster registration",
		"cluster-id", reg.ClusterID,
		"cluster-name", reg.ClusterName,
		"org-id", reg.OrganizationID,
		"cloud-url", reg.CloudURL)
	if reg.Tags == nil {
		reg.Tags = make(map[string]string)
	}
	reg.Tags["cluster_id"] = reg.ClusterID
	reg.Tags["cluster_name"] = reg.ClusterName
	reg.Tags["organization_id"] = reg.OrganizationID

	result := registrationBootOutput{cloudAuth: coordinate.CloudAuthConfig{
		Enabled:           true,
		CloudURL:          reg.CloudURL,
		PrivateKey:        reg.PrivateKey,
		Tags:              reg.Tags,
		ClusterID:         reg.ClusterID,
		DNSHostname:       reg.DNSHostname,
		IdentityIssuerURL: reg.IdentityIssuerURL,
	}, anchor: reg.IdentityAnchor}
	return result, nil
}
