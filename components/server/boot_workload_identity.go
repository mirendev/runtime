//go:build linux

package server

import (
	"context"
	"log/slog"

	"miren.dev/runtime/pkg/readiness"
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
	component    *readiness.Component
	inputs       workloadIdentityBootInputs
	registration *registrationBoot
	result       workloadIdentityBootOutput
}

func workloadIdentityInputs(options StartOptions) workloadIdentityBootInputs {
	return workloadIdentityBootInputs{
		log:             options.Log,
		dataPath:        options.Config.Server.GetDataPath(),
		additionalNames: append([]string(nil), options.Config.TLS.AdditionalNames...),
	}
}

func newWorkloadIdentityBoot(inputs workloadIdentityBootInputs, registration *registrationBoot) *workloadIdentityBoot {
	b := &workloadIdentityBoot{inputs: inputs, registration: registration}
	b.component = readiness.NewComponent("workload-identity", readiness.Spec{
		Dependencies: []readiness.Dependency{readiness.ReadyDep(registration.component)},
		Start:        b.start,
	})
	return b
}

func (b *workloadIdentityBoot) output() workloadIdentityBootOutput {
	return b.result
}

func (b *workloadIdentityBoot) start(context.Context, readiness.Reporter) error {
	registrationOutput := b.registration.output()
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
		b.inputs.log.Warn("failed to initialize workload identity issuer", "error", err)
		return nil
	}
	b.result.issuer = issuer
	if issuerURL == workloadidentity.LocalIssuerURL {
		b.inputs.log.Info("workload identity issuer initialized with a cluster-local anchor; "+
			"internal authentication is active, external federation needs a hostname",
			"issuer", issuerURL)
	} else {
		b.inputs.log.Info("workload identity issuer initialized", "issuer", issuerURL)
	}
	return nil
}
