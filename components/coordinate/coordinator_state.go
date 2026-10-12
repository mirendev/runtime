package coordinate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/httpingress"
)

type credentialPair struct {
	Cert []byte `json:"cert"`
	Key  []byte `json:"key"`
}

// The authority is the bootstrap record: encrypting it with its own key would
// make an etcd-only restore impossible. Snapshots therefore contain signing and
// decryption material and must be protected accordingly.
func (c *Foundation) initializeCoordinatorState(ctx context.Context) error {
	key := strings.TrimRight(c.Prefix, "/") + "/cluster-state/authority"
	res, err := c.etcdClient.Get(ctx, key)
	if err != nil {
		return err
	}
	var data []byte
	if len(res.Kvs) != 0 {
		data = res.Kvs[0].Value
	} else {
		if err := c.LoadCA(ctx); err != nil {
			return err
		}
		cert, privateKey, err := c.authority.ExportPEM()
		if err != nil {
			return err
		}
		data, err = json.Marshal(credentialPair{Cert: cert, Key: privateKey})
		if err != nil {
			return err
		}
		txn, err := c.etcdClient.Txn(ctx).If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).Then(clientv3.OpPut(key, string(data))).Else(clientv3.OpGet(key)).Commit()
		if err != nil {
			return err
		}
		if !txn.Succeeded {
			data = txn.Responses[0].GetResponseRange().Kvs[0].Value
		}
	}
	var authority credentialPair
	if err := json.Unmarshal(data, &authority); err != nil {
		return err
	}
	c.authority, err = caauth.LoadFromPEM(authority.Cert, authority.Key)
	if err != nil {
		return err
	}
	if err := c.writeCredentialPair("ca", authority); err != nil {
		return err
	}
	c.clusterState, err = clusterstate.New(c.etcdClient, c.Prefix, authority.Key)
	if err != nil {
		return err
	}

	data, err = c.clusterState.Get(ctx, "api-certificate")
	if err == nil {
		var pair credentialPair
		if err := json.Unmarshal(data, &pair); err != nil {
			return err
		}
		if err := c.writeCredentialPair("api", pair); err != nil {
			return err
		}
	} else if !errors.Is(err, clusterstate.ErrNotFound) {
		return err
	}
	if err := c.LoadAPICert(ctx); err != nil {
		return err
	}
	data, err = json.Marshal(credentialPair{Cert: c.apiCert, Key: c.apiKey})
	if err != nil {
		return err
	}
	if err := c.clusterState.Put(ctx, "api-certificate", data); err != nil {
		return err
	}
	if err := c.initializeRegistration(ctx); err != nil {
		return err
	}
	if c.WorkloadIssuer != nil {
		if err := c.initializeWorkloadIdentity(ctx); err != nil {
			return err
		}
	}
	data, err = c.clusterState.Get(ctx, "oidc-signing-key")
	if errors.Is(err, clusterstate.ErrNotFound) {
		data, err = httpingress.LoadOrGenerateSigningKey(c.DataPath)
		if err == nil {
			data, err = c.clusterState.Ensure(ctx, "oidc-signing-key", data)
		}
	}
	if err != nil {
		return err
	}
	if len(data) != 32 {
		return errors.New("invalid authoritative OIDC signing key")
	}
	c.oidcSigningKey = data
	return os.WriteFile(filepath.Join(c.DataPath, "server", "oidc-signing.key"), data, 0600)
}

func (c *Foundation) writeCredentialPair(name string, pair credentialPair) error {
	dir := filepath.Join(c.DataPath, "server")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".crt"), pair.Cert, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".key"), pair.Key, 0600)
}

// Local registration commands and manual workload key rotation remain inputs.
// A fingerprint distinguishes an explicit local change from an unchanged cache
// or an empty restore directory; the latter must never replace etcd state.
func (c *Foundation) syncMetadata(ctx context.Context, name string, local []byte) ([]byte, error) {
	marker := filepath.Join(c.DataPath, "server", name+".etcd-cache")
	previous, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	hash := sha256.Sum256(local)
	if err == nil && string(previous) != string(hash[:]) {
		if err := c.clusterState.Put(ctx, name, local); err != nil {
			return nil, err
		}
		return local, nil
	}
	return c.clusterState.Ensure(ctx, name, local)
}

func (c *Foundation) cacheMetadata(name string, data []byte) error {
	hash := sha256.Sum256(data)
	return os.WriteFile(filepath.Join(c.DataPath, "server", name+".etcd-cache"), hash[:], 0600)
}

func (c *Foundation) initializeRegistration(ctx context.Context) error {
	dir := filepath.Join(c.DataPath, "server")
	reg, err := registration.LoadRegistration(dir)
	if err != nil {
		return err
	}
	local, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	data, err := c.syncMetadata(ctx, "registration", local)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return err
	}
	if reg == nil {
		if err := registration.ClearRegistration(dir); err != nil {
			return err
		}
	} else if err := registration.SaveRegistration(dir, reg); err != nil {
		return err
	}
	if reg != nil && reg.Status == "approved" {
		tags := reg.Tags
		if tags == nil {
			tags = make(map[string]string)
		}
		tags["cluster_id"], tags["cluster_name"], tags["organization_id"] = reg.ClusterID, reg.ClusterName, reg.OrganizationID
		c.CloudAuth = CloudAuthConfig{Enabled: true, CloudURL: reg.CloudURL, PrivateKey: reg.PrivateKey, Tags: tags, ClusterID: reg.ClusterID, DNSHostname: reg.DNSHostname, IdentityIssuerURL: reg.IdentityIssuerURL}
	}
	return c.cacheMetadata("registration", data)
}

func identityFiles(dir string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	names := []string{"workload-identity.key", "workload-identity.key.prev", workloadidentity.CurrentIssuerFile, workloadidentity.PrevIssuerFile}
	entries, err := os.ReadDir(filepath.Join(dir, "workload-identity.d"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, filepath.Join("workload-identity.d", entry.Name()))
		}
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

func (c *Foundation) initializeWorkloadIdentity(ctx context.Context) error {
	dir := filepath.Join(c.DataPath, "server")
	files, err := identityFiles(dir)
	if err != nil {
		return err
	}
	local, err := json.Marshal(files)
	if err != nil {
		return err
	}
	data, err := c.syncMetadata(ctx, "workload-identity", local)
	if err != nil {
		return err
	}
	var authoritative map[string][]byte
	if err := json.Unmarshal(data, &authoritative); err != nil {
		return err
	}
	for name := range files {
		if _, ok := authoritative[name]; !ok {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	for name, contents := range authoritative {
		if _, ok := files[name]; !ok && name != "workload-identity.key" && name != "workload-identity.key.prev" && name != workloadidentity.CurrentIssuerFile && name != workloadidentity.PrevIssuerFile && (filepath.Dir(name) != "workload-identity.d" || filepath.Base(name) == ".") {
			return fmt.Errorf("invalid workload identity file %q", name)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, contents, 0600); err != nil {
			return err
		}
	}
	reg, err := registration.LoadRegistration(dir)
	if err != nil {
		return err
	}
	issuerURL := c.WorkloadIssuer.IssuerURL()
	if reg != nil && reg.Status == "approved" {
		if reg.IdentityAnchor == registration.AnchorCloud && reg.IdentityIssuerURL != "" {
			issuerURL = reg.IdentityIssuerURL
		} else if reg.DNSHostname != "" {
			issuerURL = "https://" + reg.DNSHostname
		}
	}
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{DataPath: c.DataPath, IssuerURL: issuerURL, OrganizationID: c.CloudAuth.Tags["organization_id"], ClusterID: c.CloudAuth.ClusterID})
	if err != nil {
		return err
	}
	c.WorkloadIssuer.Adopt(issuer)
	files, err = identityFiles(dir)
	if err != nil {
		return err
	}
	data, err = json.Marshal(files)
	if err != nil {
		return err
	}
	if err := c.clusterState.Put(ctx, "workload-identity", data); err != nil {
		return err
	}
	return c.cacheMetadata("workload-identity", data)
}

func (c *Foundation) OIDCSigningKey() []byte {
	return append([]byte(nil), c.oidcSigningKey...)
}
