package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/connectors"
)

// Preserve the CLI's config_json shape, but reconstruct it exclusively from
// safe RPC metadata rather than exposing arbitrary stored connector JSON.
func providerConfigJSON(p *ingress_v1alpha.ProviderInfo) string {
	if p.Kind() != "github" {
		return ""
	}
	var cfg struct {
		Orgs []connectors.GitHubOrg `json:"orgs,omitempty"`
	}
	for _, org := range p.Orgs() {
		cfg.Orgs = append(cfg.Orgs, connectors.GitHubOrg{Name: org.Name(), Teams: org.Teams()})
	}
	b, _ := json.Marshal(cfg) // Only strings and slices; marshaling cannot fail.
	return string(b)
}

func summarizeProviderOrgs(orgs []*ingress_v1alpha.GitHubOrganization) string {
	if len(orgs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(orgs))
	for _, o := range orgs {
		if len(o.Teams()) == 0 {
			parts = append(parts, o.Name())
		} else {
			parts = append(parts, fmt.Sprintf("%s[%s]", o.Name(), strings.Join(o.Teams(), ",")))
		}
	}
	return "orgs: " + strings.Join(parts, ", ")
}
