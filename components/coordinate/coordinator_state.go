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
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/atomicfile"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/etcdaccess"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/httpingress"
)

type credentialPair struct {
	Cert []byte `json:"cert"`
	Key  []byte `json:"key"`
}

func (c *Foundation) connectEtcd() (*clientv3.Client, error) {
	config := clientv3.Config{Endpoints: c.EtcdEndpoints, AutoSyncInterval: time.Minute}
	if c.EtcdTLS != nil {
		tls, err := c.buildEtcdTLSConfig()
		if err != nil {
			return nil, err
		}
		config.TLS = tls
	}
	return clientv3.New(config)
}

// PrepareMetadata hydrates caches before registration and workload-identity
// boot consumers run. No API or controllers are started here.
func (c *Foundation) PrepareMetadata(ctx context.Context) error {
	client, err := c.connectEtcd()
	if err != nil {
		return err
	}
	defer client.Close()
	c.etcdClient = client
	defer func() { c.etcdClient = nil }()
	if err := c.initializeAuthority(ctx); err != nil {
		return err
	}
	if err := c.initializeRegistration(ctx); err != nil {
		return err
	}
	return c.initializeWorkloadIdentity(ctx)
}

func (c *Foundation) prepareRunnerEtcdAccess(ctx context.Context, ec *aes.Client) error {
	if c.EtcdTLS == nil {
		return nil
	}
	response, err := ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return err
	}
	for response.Next() {
		var node compute_v1alpha.Node
		node.Decode(response.Entity())
		if node.RunnerId != "" {
			if err := etcdaccess.Runner(ctx, c.etcdClient, "runner-"+node.RunnerId); err != nil {
				return err
			}
		}
	}
	return nil
}

// The authority is sealed with a host-only key, just like the other metadata.
// A restore needs that key separately; an etcd snapshot alone cannot mint
// identities or decrypt the cluster's secrets.
func (c *Foundation) initializeAuthority(ctx context.Context) error {
	if c.EtcdTLS != nil {
		if err := etcdaccess.Ensure(ctx, c.etcdClient, c.Prefix); err != nil {
			return fmt.Errorf("securing coordinator metadata: %w", err)
		}
	}
	key, err := clusterstate.EnsureSealingKey(ctx, c.Log, c.etcdClient, c.Prefix, c.DataPath)
	if err != nil {
		return err
	}
	c.sealingKey = key
	c.clusterState, err = clusterstate.New(c.etcdClient, c.Prefix, key)
	if err != nil {
		return err
	}
	data, err := c.clusterState.Get(ctx, "authority")
	if errors.Is(err, clusterstate.ErrNotFound) {
		if err := c.LoadCA(ctx); err != nil {
			return err
		}
		var cert, privateKey []byte
		cert, privateKey, err = c.authority.ExportPEM()
		if err != nil {
			return err
		}
		data, err = json.Marshal(credentialPair{Cert: cert, Key: privateKey})
		if err != nil {
			return err
		}
		data, err = c.clusterState.Ensure(ctx, "authority", data)
	}
	if err != nil {
		return err
	}
	var authority credentialPair
	if err := json.Unmarshal(data, &authority); err != nil {
		return err
	}
	c.authority, err = caauth.LoadFromPEM(authority.Cert, authority.Key)
	if err != nil {
		return err
	}
	return writeCredentialPair(c.DataPath, "ca", authority)
}

func (c *Foundation) initializeCoordinatorState(ctx context.Context) error {
	if err := c.initializeAuthority(ctx); err != nil {
		return err
	}

	data, err := c.clusterState.Get(ctx, "api-certificate")
	if err == nil {
		var pair credentialPair
		if err := json.Unmarshal(data, &pair); err != nil {
			return err
		}
		if err := writeCredentialPair(c.DataPath, "api", pair); err != nil {
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
	if err := c.initializeWorkloadIdentity(ctx); err != nil {
		return err
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
	return atomicfile.Write(filepath.Join(c.DataPath, "server", "oidc-signing.key"), data, 0600)
}

func writeCredentialPair(dataPath, name string, pair credentialPair) error {
	dir := filepath.Join(dataPath, "server")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if name == "ca" {
		// Bootstrap reads this single generation before etcd is reachable. The
		// separate PEM files are compatibility mirrors, not a two-file commit.
		data, err := json.Marshal(pair)
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(dir, "ca.bundle"), data, 0600); err != nil {
			return err
		}
	}
	if err := atomicfile.Write(filepath.Join(dir, name+".crt"), pair.Cert, 0644); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, name+".key"), pair.Key, 0600)
}

// Local registration commands and manual workload key rotation remain inputs.
// A fingerprint distinguishes an explicit local change from an unchanged cache
// or an empty restore directory; the latter must never replace etcd state.
func (c *Foundation) syncMetadata(ctx context.Context, name string, local []byte) ([]byte, error) {
	pending := filepath.Join(c.DataPath, "server", name+".etcd-hydrating")
	if _, err := os.Stat(pending); err == nil {
		data, err := c.clusterState.Get(ctx, name)
		if errors.Is(err, clusterstate.ErrNotFound) {
			// Managed rollback may restore pre-migration etcd while leaving
			// the marker on disk. Retry the import, not a local edit.
			return c.clusterState.Ensure(ctx, name, local)
		}
		return data, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	marker := filepath.Join(c.DataPath, "server", name+".etcd-cache")
	previous, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	hash := sha256.Sum256(local)
	var data []byte
	if err == nil && string(previous) != string(hash[:]) {
		if err := c.clusterState.Put(ctx, name, local); err != nil {
			return nil, err
		}
		data = local
	} else {
		data, err = c.clusterState.Ensure(ctx, name, local)
		if err != nil {
			return nil, err
		}
	}
	// An interrupted hydration is not a local administrative edit.
	if err := atomicfile.Write(pending, nil, 0600); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Foundation) cacheMetadata(name string, data []byte) error {
	hash := sha256.Sum256(data)
	dir := filepath.Join(c.DataPath, "server")
	if err := atomicfile.Write(filepath.Join(dir, name+".etcd-cache"), hash[:], 0600); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, name+".etcd-hydrating")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
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
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
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
		if err := atomicfile.Write(path, contents, 0600); err != nil {
			return err
		}
	}
	if c.WorkloadIssuer == nil {
		return c.cacheMetadata("workload-identity", data)
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
