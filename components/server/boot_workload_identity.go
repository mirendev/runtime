//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/workloadidentity"
)

type workloadIdentityBootInputs struct {
	log             *slog.Logger
	dataPath        string
	additionalNames []string
}

type workloadIdentityBootOutput struct {
	issuer *workloadidentity.Issuer
}

type workloadIdentityBoot struct {
	component *boot.Component
	inputs    workloadIdentityBootInputs
	output    boot.Output[workloadIdentityBootOutput]
}

func workloadIdentityInputs(options StartOptions) workloadIdentityBootInputs {
	return workloadIdentityBootInputs{
		log:             options.Log,
		dataPath:        options.Config.Server.GetDataPath(),
		additionalNames: append([]string(nil), options.Config.TLS.AdditionalNames...),
	}
}

func newWorkloadIdentityBoot(inputs workloadIdentityBootInputs, registration boot.Output[registrationBootOutput]) *workloadIdentityBoot {
	b := &workloadIdentityBoot{inputs: inputs}
	b.component, b.output = boot.Provide1("workload-identity", registration, b.start)
	return b
}

func (b *workloadIdentityBoot) start(_ context.Context, registrationOutput registrationBootOutput) (workloadIdentityBootOutput, error) {
	// The signing key always stays on this server. The registration anchor only
	// decides who publishes discovery and which URL goes in the iss claim.
	anchor := registrationOutput.anchor
	if anchor == "" {
		anchor = registration.AnchorCluster
	}
	anchorAtCloud := anchor == registration.AnchorCloud

	issuerURL := workloadidentity.LocalIssuerURL
	switch {
	case anchorAtCloud && registrationOutput.cloudAuth.IdentityIssuerURL != "":
		issuerURL = registrationOutput.cloudAuth.IdentityIssuerURL
	case registrationOutput.cloudAuth.DNSHostname != "":
		issuerURL = "https://" + registrationOutput.cloudAuth.DNSHostname
	case len(b.inputs.additionalNames) > 0:
		issuerURL = "https://" + b.inputs.additionalNames[0]
	}

	if anchorAtCloud && registrationOutput.cloudAuth.IdentityIssuerURL == "" {
		b.inputs.log.Warn("workload identity anchor is set to cloud but cloud assigned none; "+
			"falling back to the cluster's own anchor. Re-register, or check that "+
			"miren.cloud has IDENTITY_ISSUER_BASE_URL configured",
			"issuer", issuerURL)
	}

	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:       b.inputs.dataPath,
		IssuerURL:      issuerURL,
		OrganizationID: registrationOutput.cloudAuth.Tags["organization_id"],
		ClusterID:      registrationOutput.cloudAuth.ClusterID,
	})
	if err != nil {
		return workloadIdentityBootOutput{}, fmt.Errorf("initializing workload identity issuer: %w", err)
	}
	if issuerURL == workloadidentity.LocalIssuerURL {
		b.inputs.log.Info("workload identity issuer initialized with a cluster-local anchor; "+
			"internal authentication is active, external federation needs a hostname",
			"issuer", issuerURL)
	} else {
		b.inputs.log.Info("workload identity issuer initialized", "issuer", issuerURL)
	}
	return workloadIdentityBootOutput{issuer: issuer}, nil
}
