package commands

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"miren.dev/mflags"
)

type CloudClusterOptions struct {
	CloudOrganizationOptions
	Cluster string `long:"cluster" description:"Cloud cluster XID (not a local config name)"`
}

type CloudGroupOptions struct {
	CloudOptions
	Group string `long:"group" description:"Cloud group XID"`
}

type CloudInviteOptions struct {
	CloudOrganizationOptions
	Invite string `long:"invite" description:"Invite link ID"`
}

type CloudInviteInput struct {
	Expires string `long:"expires-in" description:"Invite lifetime: 1d, 7d, 30d or never (create defaults to 7d)" choice:"1d" choice:"7d" choice:"30d" choice:"never"` //nolint
	MaxUses string `long:"max-uses" description:"Maximum uses as a JSON integer, or null for unlimited; omitted leaves unchanged on update"`
	Groups  string `long:"groups" description:"JSON array of group XIDs; [] clears groups on update"`
}

type CloudSubdomainOptions struct {
	CloudOrganizationOptions
	Subdomain string `long:"subdomain" description:"Claimed subdomain ID"`
}

const (
	cloudOrgPath     = "/api/v1/organizations/{organization}"
	cloudClusterPath = cloudOrgPath + "/clusters/{cluster}"
	cloudGroupPath   = "/api/v1/groups/{group}"
)

func registerCloudResourceCommands(d *mflags.Dispatcher) {
	for _, section := range []struct{ name, synopsis string }{
		{"cloud account", "Cloud profile and credentials"},
		{"cloud account identity", "Linked sign-in identities"},
		{"cloud account key", "Cloud authentication keys"},
		{"cloud account token", "Cloud token revocation"},
		{"cloud user", "Cloud user discovery"},
		{"cloud org member", "Cloud organization membership"},
		{"cloud group", "Cloud access groups"},
		{"cloud group member", "Cloud group membership"},
		{"cloud invite", "Organization invite links and recipient actions"},
		{"cloud enroll-token", "Single-use cluster enrollment credentials"},
		{"cloud subdomain", "Cloud subdomain claims and assignments"},
		{"cloud volume", "Cloud volume and backup inventory"},
	} {
		d.Dispatch(section.name, Section(section.name, section.synopsis, ""))
	}
	d.Dispatch("cloud me", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud me", Synopsis: "Show your Cloud profile, organizations and enabled features", Path: "/api/v1/me",
		Render: func(ctx *Context, response any) error {
			ctx.Printf("Profile\n")
			for _, field := range []CloudColumn{{"Name", "user.name"}, {"Email", "user.email"}, {"ID", "user.id"}, {"Avatar", "user.avatar_url"}, {"Created", "user.created_at"}, {"Updated", "user.updated_at"}, {"Sign-in provider", "signed_in_with.provider"}, {"Sign-in account", "signed_in_with.handle"}} {
				if value := cloudField(response, field.Field); value != nil {
					ctx.Printf("%s: %v\n", field.Header, value)
				}
			}
			if cloudField(response, "is_superadmin") == true {
				ctx.Printf("Superadmin: yes\n")
			}
			ctx.Printf("\nOrganizations\n")
			if cloudField(response, "organizations") == nil {
				ctx.Printf("Not reported\n")
			} else {
				organizations, ok := cloudField(response, "organizations").([]any)
				if !ok {
					return fmt.Errorf("cloud response field %q is not a list", "organizations")
				}
				var rows []any
				for _, org := range organizations {
					features := "not reported"
					if flags, ok := cloudField(org, "feature_flags").(map[string]any); ok {
						var enabled []string
						for name, value := range flags {
							if value == true {
								enabled = append(enabled, name)
							}
						}
						sort.Strings(enabled)
						features = "none"
						if len(enabled) > 0 {
							features = strings.Join(enabled, ", ")
						}
					}
					rows = append(rows, map[string]any{"id": cloudField(org, "id"), "name": cloudField(org, "name"), "role": cloudField(org, "role"), "features": features})
				}
				if err := (CloudEndpoint{List: "organizations", Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"ROLE", "role"}, {"ENABLED FEATURES", "features"}}}).render(ctx, map[string]any{"organizations": rows}); err != nil {
					return err
				}
			}
			ctx.Printf("\nLinked identities\n")
			if cloudField(response, "identities") == nil {
				ctx.Printf("Not reported\n")
				return nil
			}
			return (CloudEndpoint{List: "identities", Columns: []CloudColumn{{"PROVIDER", "provider"}, {"ACCOUNT", "username"}, {"EMAIL", "email"}, {"PROVIDER ID", "id"}, {"CONNECTED", "connected_at"}}}).render(ctx, response)
		},
	}))
	d.Dispatch("cloud account update", CloudCommand[struct {
		CloudOptions
		Name        string `long:"name" description:"New display name; omitted preserves it"`
		Email       string `long:"email" description:"New email address; omitted preserves it"`
		Avatar      string `long:"avatar-url" description:"New avatar URL; omitted preserves it"`
		ClearAvatar bool   `long:"clear-avatar" description:"Remove your avatar"`
	}](CloudEndpoint{
		Name: "cloud account update", Synopsis: "Update your Cloud profile, preserving omitted fields", Method: http.MethodPut, Path: "/api/v1/me",
		Inputs:   map[string]string{"name": "body:name", "email": "body:email", "avatar-url": "body:avatar_url", "clear-avatar": "clear:avatar_url"},
		Preserve: map[string]string{"name": "user.name", "email": "user.email", "avatar_url": "user.avatar_url"},
	}))
	d.Dispatch("cloud account identity list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud account identity list", Synopsis: "List your linked sign-in identities", Path: "/api/v1/identities", List: "identities",
		Columns: []CloudColumn{{"PROVIDER", "provider"}, {"ID", "id"}, {"USERNAME", "username"}, {"EMAIL", "email"}, {"CONNECTED", "connected_at"}},
	}))
	d.Dispatch("cloud account identity unlink", CloudCommand[struct {
		CloudDeleteOptions
		Provider string `long:"provider" description:"Sign-in provider" choice:"github" choice:"google"` //nolint
		ID       string `long:"provider-id" description:"Provider's account ID from identity list (not a Cloud user XID)"`
	}](CloudEndpoint{
		Name: "cloud account identity unlink", Synopsis: "Unlink a sign-in identity (cannot remove the last one)", Method: http.MethodDelete, Path: "/api/v1/identities/{provider}/{provider-id}",
		Confirmation: "Unlink this identity so it can no longer sign in to your account?",
	}))
	d.Dispatch("cloud account key list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud account key list", Synopsis: "List your Cloud authentication keys, including revoked keys", Path: "/api/v1/users/keys", List: "keys",
		Columns: []CloudColumn{{"ID", "xid"}, {"NAME", "name"}, {"TYPE", "key_type"}, {"STATUS", "status"}, {"PRIMARY", "is_primary"}, {"LAST USED", "last_used_at"}},
	}))
	d.Dispatch("cloud account key revoke", CloudCommand[struct {
		CloudDeleteOptions
		Key string `long:"key" description:"Cloud authentication key XID from key list"`
	}](CloudEndpoint{
		Name: "cloud account key revoke", Synopsis: "Revoke a Cloud authentication key without removing local credentials", Method: http.MethodDelete, Path: "/api/v1/users/keys/{key}",
		Confirmation: "Revoke this authentication key? Clients using it will lose access.",
	}))
	d.Dispatch("cloud account token revoke-all", CloudCommand[struct {
		CloudDeleteOptions
		Reason string `long:"reason" description:"Optional revocation reason"`
	}](CloudEndpoint{
		Name: "cloud account token revoke-all", Synopsis: "Revoke all previously issued tokens for your account", Method: http.MethodPost, Path: "/auth/revoke/user",
		Inputs:       map[string]string{"reason": "body:reason,include-empty"},
		Confirmation: "Revoke all your existing tokens? Sessions will lose access; local credentials are not removed.",
	}))
	d.Dispatch("cloud user search", CloudCommand[struct {
		CloudOptions
		Query string `long:"query" description:"Name, email or user XID to search across Cloud"`
	}](CloudEndpoint{
		Name: "cloud user search", Synopsis: "Find existing Cloud users to add to an organization", Path: "/api/v1/users/search",
		Inputs: map[string]string{"query": "query:q,required"}, List: "users", Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"EMAIL", "email"}},
	}))
	for _, endpoint := range []CloudEndpoint{
		{Name: "cloud org show", Synopsis: "Show organization details and membership", Path: cloudOrgPath},
		{Name: "cloud org member list", Synopsis: "List organization members", Path: cloudOrgPath, List: "members",
			Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"EMAIL", "email"}, {"ROLE", "role"}}},
		{Name: "cloud org clusters", Synopsis: "List clusters and mirrored status in an organization", Path: cloudOrgPath + "/clusters", List: "clusters",
			Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"STATE", "state"}, {"NODES", "node_count"}, {"APPS", "app_count"}}},
		{Name: "cloud group list", Synopsis: "List organization access groups", Path: cloudOrgPath + "/groups", List: "groups",
			Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"MEMBERS", "member_count"}, {"DEFAULT", "is_default"}}},
		{Name: "cloud invite list", Synopsis: "List outstanding organization invite links (admin only)", Path: cloudOrgPath + "/invite-links", List: "invite_links",
			Columns: []CloudColumn{{"ID", "id"}, {"EXPIRES", "expires_at"}, {"USES", "use_count"}, {"MAX USES", "max_uses"}}},
	} {
		d.Dispatch(endpoint.Name, CloudCommand[CloudOrganizationOptions](endpoint))
	}
	d.Dispatch("cloud org create", CloudCommand[struct {
		CloudOptions
		Name string `long:"name" description:"New organization name"`
	}](CloudEndpoint{
		Name: "cloud org create", Synopsis: "Create an organization and become its admin", Method: http.MethodPost, Path: "/api/v1/organizations",
		Inputs: map[string]string{"name": "body:name,required"},
	}))
	d.Dispatch("cloud org member add", CloudCommand[struct {
		CloudOrganizationOptions
		User string `long:"user" description:"Existing user XID from cloud user search (admin only)"`
	}](CloudEndpoint{
		Name: "cloud org member add", Synopsis: "Add an existing Cloud user to an organization (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/members",
		Inputs: map[string]string{"user": "body:user_id,required"},
	}))
	d.Dispatch("cloud org member set-role", CloudCommand[struct {
		CloudOrganizationOptions
		User string `long:"user" description:"Cloud user XID"`
		Role string `long:"role" description:"Organization role: admin or member" choice:"admin" choice:"member"` //nolint
	}](CloudEndpoint{
		Name: "cloud org member set-role", Synopsis: "Set an organization member's role (admin only)", Method: http.MethodPut, Path: cloudOrgPath + "/users/{user}/role",
		Inputs: map[string]string{"role": "body:role,required"},
	}))
	d.Dispatch("cloud org member remove", CloudCommand[struct {
		CloudDeleteOptions
		Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
		User         string `long:"user" description:"Cloud user XID"`
	}](CloudEndpoint{
		Name: "cloud org member remove", Synopsis: "Remove an organization member (admin only)", Method: http.MethodDelete, Path: cloudOrgPath + "/members/{user}",
		Confirmation: "Remove this user's access to the organization?",
	}))
	d.Dispatch("cloud org user-search", CloudCommand[struct {
		CloudOrganizationOptions
		Query string `long:"query" description:"User name or email to search for"`
	}](CloudEndpoint{
		Name: "cloud org user-search", Synopsis: "Search existing organization members for group membership", Path: cloudOrgPath + "/users/search",
		Inputs: map[string]string{"query": "query:q,required"}, List: "users", Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"EMAIL", "email"}},
	}))

	d.Dispatch("cloud cluster delete", CloudCommand[struct {
		CloudDeleteOptions
		Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
		Cluster      string `long:"cluster" description:"Cloud cluster XID"`
	}](CloudEndpoint{
		Name: "cloud cluster delete", Synopsis: "Delete a Cloud cluster record (admin only); leaves server registration files unchanged", Method: http.MethodDelete, Path: cloudClusterPath,
		Confirmation: "Delete this cluster from Cloud? Cloud access and routing will be removed; the server's local registration remains.",
	}))
	d.Dispatch("cloud cluster online-batch", CloudCommand[struct {
		CloudOptions
		Clusters string `long:"clusters" description:"Required JSON array of Cloud cluster XIDs; inaccessible clusters are omitted"`
	}](CloudEndpoint{
		Name: "cloud cluster online-batch", Synopsis: "Check connectivity for multiple accessible clusters", Method: http.MethodPost, Path: "/api/v1/cluster-channel/online",
		Inputs: map[string]string{"clusters": "json:clusters,required"}, List: "statuses",
		Columns: []CloudColumn{{"CLUSTER", "cluster_xid"}, {"ONLINE", "online"}},
	}))
	for _, endpoint := range []CloudEndpoint{
		{Name: "cloud cluster show", Synopsis: "Show cloud cluster metadata and mirrored status", Path: cloudClusterPath},
		{Name: "cloud cluster nodes", Synopsis: "Show mirrored node inventory and sync state", Path: cloudClusterPath + "/nodes"},
		{Name: "cloud cluster routes", Synopsis: "Show mirrored cluster routes and sync state", Path: cloudClusterPath + "/routes"},
		{Name: "cloud cluster server", Synopsis: "Show server lifecycle status and supported actions", Path: cloudClusterPath + "/server"},
		{Name: "cloud cluster upgrade-policy", Synopsis: "Show the cluster's automatic-upgrade policy", Path: cloudClusterPath + "/server/upgrade-policy"},
	} {
		d.Dispatch(endpoint.Name, CloudCommand[CloudClusterOptions](endpoint))
	}
	d.Dispatch("cloud cluster online", CloudCommand[struct {
		CloudOptions
		Cluster string `long:"cluster" description:"Cloud cluster XID"`
	}](CloudEndpoint{
		Name: "cloud cluster online", Synopsis: "Check cluster connectivity under your Cloud access rules", Path: "/api/v1/cluster-channel/clusters/{cluster}/online",
		Columns: []CloudColumn{{"CLUSTER", "cluster_xid"}, {"ONLINE", "online"}},
	}))
	d.Dispatch("cloud cluster update", CloudCommand[struct {
		CloudClusterOptions
		Name             string `long:"name" description:"New cluster name"`
		Description      string `long:"description" description:"New description as plain text; omitted preserves it"`
		ClearDescription bool   `long:"clear-description" description:"Remove the cluster description"`
		Tags             string `long:"tags" description:"Replacement tags as a JSON object"`
		Anywhere         string `long:"anywhere-mode" description:"Miren Anywhere mode" choice:"default" choice:"never" choice:"auto" choice:"always"` //nolint
	}](CloudEndpoint{
		Name: "cloud cluster update", Synopsis: "Update cloud cluster metadata (admin only)", Method: http.MethodPatch, Path: cloudClusterPath,
		Inputs: map[string]string{"name": "body:name", "description": "body:description", "clear-description": "clear:description", "tags": "json:tags", "anywhere-mode": "body:miren_anywhere_mode"},
	}))
	d.Dispatch("cloud cluster operations", CloudCommand[struct {
		CloudClusterOptions
		Limit int `long:"limit" description:"Maximum operations (Cloud default 50, cap 200)"`
	}](CloudEndpoint{
		Name: "cloud cluster operations", Synopsis: "List server restart and upgrade operation history", Path: cloudClusterPath + "/server/operations",
		Inputs: map[string]string{"limit": "query:limit"}, List: "operations",
		Columns: []CloudColumn{{"ID", "id"}, {"ACTION", "action"}, {"STATUS", "status"}, {"TARGET", "target_version"}, {"CREATED", "created_at"}},
	}))
	d.Dispatch("cloud cluster operation", CloudCommand[struct {
		CloudClusterOptions
		Operation string `long:"operation" description:"Server operation ID"`
	}](CloudEndpoint{
		Name: "cloud cluster operation", Synopsis: "Show a server operation's progress and outcome", Path: cloudClusterPath + "/server/operations/{operation}",
	}))

	for _, endpoint := range []CloudEndpoint{
		{Name: "cloud group show", Synopsis: "Show an access group", Path: cloudGroupPath},
		{Name: "cloud group member list", Synopsis: "List access group members", Path: cloudGroupPath + "/members", List: "members",
			Columns: []CloudColumn{{"USER", "user_id"}, {"NAME", "user_name"}, {"EMAIL", "user_email"}, {"ADDED", "added_at"}}},
	} {
		d.Dispatch(endpoint.Name, CloudCommand[CloudGroupOptions](endpoint))
	}
	d.Dispatch("cloud group create", CloudCommand[struct {
		CloudOrganizationOptions
		Name        string `long:"name" description:"Access group name"`
		Description string `long:"description" description:"Access group description"`
	}](CloudEndpoint{
		Name: "cloud group create", Synopsis: "Create an access group (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/groups",
		Inputs: map[string]string{"name": "body:name,required", "description": "body:description"},
	}))
	d.Dispatch("cloud group update", CloudCommand[struct {
		CloudGroupOptions
		Name             string `long:"name" description:"New group name; omitted preserves it"`
		Description      string `long:"description" description:"New description; omitted preserves it"`
		ClearDescription bool   `long:"clear-description" description:"Remove the group description"`
	}](CloudEndpoint{
		Name: "cloud group update", Synopsis: "Update an access group, preserving omitted fields (admin only)", Method: http.MethodPut, Path: cloudGroupPath,
		Inputs:   map[string]string{"name": "body:name", "description": "body:description", "clear-description": "clear:description"},
		Preserve: map[string]string{"name": "group.name", "description": "group.description"},
	}))
	d.Dispatch("cloud group member add", CloudCommand[struct {
		CloudGroupOptions
		User string `long:"user" description:"Cloud user XID (must already belong to the organization)"`
	}](CloudEndpoint{
		Name: "cloud group member add", Synopsis: "Add a user to an access group (admin only)", Method: http.MethodPost, Path: cloudGroupPath + "/members",
		Inputs: map[string]string{"user": "body:user_xid,required"},
	}))
	d.Dispatch("cloud group member remove", CloudCommand[struct {
		CloudDeleteOptions
		Group string `long:"group" description:"Access group XID"`
		User  string `long:"user" description:"Cloud user XID"`
	}](CloudEndpoint{
		Name: "cloud group member remove", Synopsis: "Remove a user from an access group (admin only)", Method: http.MethodDelete, Path: cloudGroupPath + "/members/{user}",
		Confirmation: "Remove this user from the access group?",
	}))
	d.Dispatch("cloud group delete", CloudCommand[struct {
		CloudDeleteOptions
		Group string `long:"group" description:"Access group XID"`
	}](CloudEndpoint{
		Name: "cloud group delete", Synopsis: "Delete a non-default access group (admin only)", Method: http.MethodDelete, Path: cloudGroupPath,
		Confirmation: "Delete this access group and its memberships?",
	}))

	for _, endpoint := range []CloudEndpoint{
		{Name: "cloud invite show", Synopsis: "Inspect an invitation as its recipient", Path: "/api/v1/invitations/{token}"},
		{Name: "cloud invite accept", Synopsis: "Join an organization using an invitation", Method: http.MethodPost, Path: "/api/v1/invitations/{token}/accept"},
	} {
		d.Dispatch(endpoint.Name, CloudCommand[struct {
			CloudOptions
			Token string `long:"token" description:"Credential token from the invitation URL (not an invite link ID)"`
		}](endpoint))
	}
	inviteInputs := map[string]string{"expires-in": "body:expires_in", "max-uses": "json:max_uses", "groups": "json:groups"}
	d.Dispatch("cloud invite create", CloudCommand[struct {
		CloudOrganizationOptions
		CloudInviteInput
	}](CloudEndpoint{
		Name: "cloud invite create", Synopsis: "Create a shared organization invite link (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/invite-link", Inputs: inviteInputs,
	}))
	d.Dispatch("cloud invite update", CloudCommand[struct {
		CloudInviteOptions
		CloudInviteInput
	}](CloudEndpoint{
		Name: "cloud invite update", Synopsis: "Update an invite's expiry, use cap or groups (admin only)", Method: http.MethodPatch, Path: cloudOrgPath + "/invite-links/{invite}", Inputs: inviteInputs,
	}))
	d.Dispatch("cloud invite url", CloudCommand[CloudInviteOptions](CloudEndpoint{
		Name: "cloud invite url", Synopsis: "Retrieve a copyable invite URL (admin only)", Path: cloudOrgPath + "/invite-links/{invite}/url",
	}))
	d.Dispatch("cloud invite revoke", CloudCommand[struct {
		CloudDeleteOptions
		Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
		Invite       string `long:"invite" description:"Invite link ID"`
	}](CloudEndpoint{
		Name: "cloud invite revoke", Synopsis: "Revoke a shared organization invite link (admin only)", Method: http.MethodDelete, Path: cloudOrgPath + "/invite-links/{invite}",
		Confirmation: "Revoke this invite link so it can no longer be used?",
	}))

	d.Dispatch("cloud enroll-token create", CloudCommand[struct {
		CloudOrganizationOptions
		TTL  int    `long:"ttl-hours" description:"Token lifetime in hours (Cloud default 24, maximum 168)"`
		Tags string `long:"default-tags" description:"Default cluster tags as a JSON object"`
	}](CloudEndpoint{
		Name: "cloud enroll-token create", Synopsis: "Mint a cluster enrollment credential and bootstrap config (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/enroll-tokens",
		Inputs: map[string]string{"ttl-hours": "body:ttl_hours", "default-tags": "json:default_tags"},
	}))
	d.Dispatch("cloud enroll-token list", CloudCommand[struct {
		CloudOrganizationOptions
		Limit  int `long:"limit" description:"Maximum tokens (Cloud default 50, cap 200)"`
		Offset int `long:"offset" description:"Number of tokens to skip"`
	}](CloudEndpoint{
		Name: "cloud enroll-token list", Synopsis: "List enrollment token status without secrets", Path: cloudOrgPath + "/enroll-tokens",
		Inputs: map[string]string{"limit": "query:limit", "offset": "query:offset"}, List: "enroll_tokens",
		Columns: []CloudColumn{{"ID", "id"}, {"STATE", "state"}, {"EXPIRES", "expires_at"}, {"CLUSTER", "used_by_cluster_id"}},
	}))
	d.Dispatch("cloud enroll-token show", CloudCommand[struct {
		CloudOrganizationOptions
		Token string `long:"token" description:"Enrollment token ID (not its secret)"`
	}](CloudEndpoint{
		Name: "cloud enroll-token show", Synopsis: "Show enrollment token state and bootstrap progress", Path: cloudOrgPath + "/enroll-tokens/{token}",
	}))
	d.Dispatch("cloud enroll-token revoke", CloudCommand[struct {
		CloudDeleteOptions
		Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
		Token        string `long:"token" description:"Enrollment token ID (not its secret)"`
	}](CloudEndpoint{
		Name: "cloud enroll-token revoke", Synopsis: "Revoke an unused cluster enrollment token (admin only)", Method: http.MethodDelete, Path: cloudOrgPath + "/enroll-tokens/{token}",
		Confirmation: "Revoke this token so no cluster can join with it?",
	}))

	d.Dispatch("cloud subdomain domains", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud subdomain domains", Synopsis: "List available subdomain base domains", Path: "/api/v1/subdomains/base-domains", List: "domains",
		Columns: []CloudColumn{{"DOMAIN", "domain"}, {"ENABLED", "enabled"}},
	}))
	d.Dispatch("cloud subdomain list", CloudCommand[CloudOrganizationOptions](CloudEndpoint{
		Name: "cloud subdomain list", Synopsis: "List claimed organization subdomains", Path: cloudOrgPath + "/subdomains", List: "subdomains",
		Columns: []CloudColumn{{"ID", "id"}, {"HOSTNAME", "full_hostname"}, {"CLUSTER", "assignment.cluster_name"}, {"STATUS", "assignment.status"}},
	}))
	d.Dispatch("cloud subdomain claim", CloudCommand[struct {
		CloudOrganizationOptions
		Name string `long:"name" description:"Subdomain name to claim"`
		Base string `long:"base-domain" description:"Base domain (defaults to Cloud's first enabled domain)"`
	}](CloudEndpoint{
		Name: "cloud subdomain claim", Synopsis: "Claim a subdomain for an organization (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/subdomains",
		Inputs: map[string]string{"name": "body:name,required", "base-domain": "body:base_domain"},
	}))
	d.Dispatch("cloud subdomain assign", CloudCommand[struct {
		CloudSubdomainOptions
		Cluster string `long:"cluster" description:"Cloud cluster XID to serve this subdomain"`
	}](CloudEndpoint{
		Name: "cloud subdomain assign", Synopsis: "Assign a subdomain to a cluster (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/subdomains/{subdomain}/assign",
		Inputs: map[string]string{"cluster": "body:cluster_id,required"},
	}))
	for _, endpoint := range []CloudEndpoint{
		{Name: "cloud subdomain unassign", Synopsis: "Remove a subdomain's cluster assignment (admin only)", Method: http.MethodPost, Path: cloudOrgPath + "/subdomains/{subdomain}/unassign",
			Confirmation: "Remove this subdomain's cluster assignment and stop routing its traffic?"},
		{Name: "cloud subdomain release", Synopsis: "Release an organization's subdomain claim (admin only)", Method: http.MethodDelete, Path: cloudOrgPath + "/subdomains/{subdomain}",
			Confirmation: "Release this subdomain and its DNS routing?"},
	} {
		d.Dispatch(endpoint.Name, CloudCommand[struct {
			CloudDeleteOptions
			Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
			Subdomain    string `long:"subdomain" description:"Claimed subdomain ID"`
		}](endpoint))
	}

	d.Dispatch("cloud volume list", CloudCommand[struct {
		CloudOrganizationOptions
		Cursor string `long:"cursor" description:"Cursor from the previous response"`
		Limit  int    `long:"limit" description:"Page size (Cloud default 50, range 1-100)"`
	}](CloudEndpoint{
		Name: "cloud volume list", Synopsis: "List cloud volumes and active leases", Path: cloudOrgPath + "/volumes",
		Inputs: map[string]string{"cursor": "query:cursor", "limit": "query:limit"}, List: "volumes",
		Columns: []CloudColumn{{"ID", "volume_id"}, {"NAME", "name"}, {"SIZE", "declared_size"}, {"LEASE", "lease.lease_id"}},
	}))
	d.Dispatch("cloud volume updates", CloudCommand[struct {
		CloudOrganizationOptions
		Volume string `long:"volume" description:"Cloud volume ID"`
		Cursor string `long:"cursor" description:"Cursor from the previous response (kind:ordering_key)"`
		Kind   string `long:"kind" description:"Filter update kind"`
		Limit  int    `long:"limit" description:"Page size (Cloud default 50, range 1-200)"`
	}](CloudEndpoint{
		Name: "cloud volume updates", Synopsis: "List a cloud volume's snapshots and update artifacts", Path: cloudOrgPath + "/volumes/{volume}/updates",
		Inputs: map[string]string{"cursor": "query:cursor", "kind": "query:kind", "limit": "query:limit"}, List: "updates",
		Columns: []CloudColumn{{"ID", "update_id"}, {"KIND", "kind"}, {"ORDERING KEY", "ordering_key"}, {"SIZE", "size"}, {"COMPLETED", "completed_at"}},
	}))
}
