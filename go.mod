module miren.dev/runtime

go 1.25.1

require (
	github.com/NimbleMarkets/ntcharts v0.3.1
	github.com/charmbracelet/bubbles v0.20.0
	github.com/charmbracelet/bubbletea v1.2.4
	github.com/charmbracelet/lipgloss v1.0.0
	github.com/cilium/ebpf v0.16.0
	github.com/coder/websocket v1.8.14
	github.com/containerd/cgroups/v3 v3.0.5
	github.com/containerd/console v1.0.4
	github.com/containerd/containerd v1.7.23
	github.com/containerd/containerd/api v1.8.0
	github.com/containerd/containerd/v2 v2.0.2
	github.com/containerd/errdefs v1.0.0
	github.com/containerd/log v0.1.0
	github.com/containerd/platforms v1.0.0-rc.1
	github.com/containerd/typeurl/v2 v2.2.3
	github.com/containernetworking/cni v1.2.3
	github.com/containernetworking/plugins v1.6.2
	github.com/corazawaf/coraza-coreruleset/v4 v4.25.0
	github.com/corazawaf/coraza/v3 v3.7.0
	github.com/coreos/go-iptables v0.8.0
	github.com/dave/jennifer v1.7.1
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/dexidp/dex v0.0.0-20260520212858-e703bc3d29c4
	github.com/docker/cli v27.5.0+incompatible
	github.com/docker/docker v27.5.1+incompatible
	github.com/docker/go-connections v0.5.0
	github.com/elastic/go-perf v0.0.0-20241029065020-30bec95324b8
	github.com/flannel-io/flannel v0.26.7
	github.com/fxamacker/cbor/v2 v2.9.2
	github.com/go-acme/lego/v4 v4.28.1
	github.com/go-git/go-git/v5 v5.18.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/go-logr/logr v1.4.3
	github.com/go-sql-driver/mysql v1.10.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/cel-go v0.28.1
	github.com/google/gofuzz v1.2.0
	github.com/google/pprof v0.0.0-20241210010833-40e02aabc2ad
	github.com/google/uuid v1.6.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/ironpark/skiplist v0.0.0-20230103051251-d63941a7d606
	github.com/jackc/pgx/v5 v5.8.0
	github.com/lucasb-eyer/go-colorful v1.2.0
	github.com/mattn/go-colorable v0.1.14
	github.com/mattn/go-isatty v0.0.20
	github.com/mattn/go-runewidth v0.0.19
	github.com/mattn/go-sqlite3 v1.14.44
	github.com/miekg/dns v1.1.68
	github.com/mitchellh/go-testing-interface v1.14.1
	github.com/mitchellh/mapstructure v1.5.0
	github.com/moby/buildkit v0.19.0
	github.com/morikuni/aec v1.0.0
	github.com/mr-tron/base58 v1.2.0
	github.com/muesli/termenv v0.15.2
	github.com/oklog/ulid/v2 v2.1.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.0
	github.com/opencontainers/runtime-spec v1.2.0
	github.com/pelletier/go-toml/v2 v2.2.4
	github.com/pkg/errors v0.9.1
	github.com/quic-go/quic-go v0.57.1
	github.com/quic-go/webtransport-go v0.9.0
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.9
	github.com/stretchr/testify v1.11.1
	github.com/tidwall/gjson v1.18.0
	github.com/tonistiigi/fsutil v0.0.0-20250113203817-b14e27f4135a
	github.com/tonistiigi/go-csvvalue v0.0.0-20240710180619-ddb21b71c0b4
	github.com/tonistiigi/units v0.0.0-20180711220420-6950e57a87ea
	github.com/tonistiigi/vt100 v0.0.0-20240514184818-90bafcd6abab
	github.com/vishvananda/netlink v1.3.1-0.20240922070040-084abd93d350
	go.etcd.io/etcd/api/v3 v3.6.11
	go.etcd.io/etcd/client/v3 v3.6.11
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.38.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.14.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.33.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.33.0
	go.opentelemetry.io/otel/log v0.14.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/log v0.14.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	go.opentelemetry.io/proto/otlp v1.7.1
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
	golang.org/x/crypto v0.51.0
	golang.org/x/net v0.54.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.44.0
	golang.org/x/term v0.43.0
	golang.org/x/text v0.37.0
	golang.org/x/time v0.15.0
	golang.org/x/tools v0.44.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260319201613-d00831a3d3e7
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/klog/v2 v2.130.1
	miren.dev/jsonrpc3/go/jsonrpc3 v0.0.0-20260106052505-c98e2702b093
	miren.dev/lbd v0.0.0-20260224020427-8914d8db2233
	miren.dev/mflags v0.0.0-20260709231109-a397dcbc98df
	sigs.k8s.io/knftables v0.0.21
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.1 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.7 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.96.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.5 // indirect
	github.com/aws/smithy-go v1.24.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.3.0 // indirect
	github.com/corazawaf/libinjection-go v0.3.2 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.8.0 // indirect
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/gotnospirit/makeplural v0.0.0-20180622080156-a5f48d94d976 // indirect
	github.com/gotnospirit/messageformat v0.0.0-20221001023931-dfe49f1eb092 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/kaptinlin/go-i18n v0.1.4 // indirect
	github.com/kaptinlin/jsonschema v0.4.6 // indirect
	github.com/magefile/mage v1.17.0 // indirect
	github.com/mdlayher/genetlink v1.3.2 // indirect
	github.com/mdlayher/netlink v1.7.3-0.20250113171957-fbb4dce95f42 // indirect
	github.com/petar-dambovaliev/aho-corasick v0.0.0-20250424160509-463d218d4745 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/shibumi/go-pathspec v1.3.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/valllabh/ocsf-schema-golang v1.0.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
)

require (
	cel.dev/expr v0.25.1 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cyphar.com/go-pathrs v0.2.1 // indirect
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20240806141605-e8a1dd7889d6 // indirect
	github.com/AdamKorcz/go-118-fuzz-build v0.0.0-20231105174938-2b5cbb29f3e2 // indirect
	github.com/AdamSLevy/jsonrpc2/v14 v14.1.0 // indirect
	github.com/Azure/azure-sdk-for-go v68.0.0+incompatible // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.19.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns v1.2.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns v1.3.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph v0.9.0 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.30 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.22 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.13 // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.4.6 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.1 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.5.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Microsoft/hcsshim v0.12.9 // indirect
	github.com/akamai/AkamaiOPEN-edgegrid-golang/v11 v11.1.0 // indirect
	github.com/alibabacloud-go/alibabacloud-gateway-spi v0.0.5 // indirect
	github.com/alibabacloud-go/darabonba-openapi/v2 v2.1.13 // indirect
	github.com/alibabacloud-go/debug v1.0.1 // indirect
	github.com/alibabacloud-go/tea v1.3.13 // indirect
	github.com/alibabacloud-go/tea-utils/v2 v2.0.7 // indirect
	github.com/aliyun/credentials-go v1.4.7 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.4 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/lightsail v1.50.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/route53 v1.59.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.6 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aziontech/azionapi-go-sdk v0.143.0 // indirect
	github.com/baidubce/bce-sdk-go v0.9.250 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/harmonica v0.2.0 // indirect
	github.com/charmbracelet/x/ansi v0.4.5 // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/clbanning/mxj/v2 v2.7.0 // indirect
	github.com/containerd/continuity v0.4.5 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/fifo v1.1.0 // indirect
	github.com/containerd/nydus-snapshotter v0.15.0 // indirect
	github.com/containerd/plugin v1.0.0 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.16.3 // indirect
	github.com/containerd/ttrpc v1.2.7 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cyphar/filepath-securejoin v0.6.0 // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dnsimple/dnsimple-go/v4 v4.0.0 // indirect
	github.com/docker/docker-credential-helpers v0.8.2 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/exoscale/egoscale/v3 v3.1.27 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-acme/alidns-20150109/v4 v4.6.1 // indirect
	github.com/go-acme/tencentclouddnspod v1.1.10 // indirect
	github.com/go-acme/tencentedgdeone v1.1.48 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.23.0 // indirect
	github.com/go-resty/resty/v2 v2.16.5 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-yaml v1.18.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gofrs/flock v0.13.0
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.15 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/gophercloud/gophercloud v1.14.1 // indirect
	github.com/gophercloud/utils v0.0.0-20231010081019-80377eca5d56 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.2 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/huaweicloud/huaweicloud-sdk-go-v3 v0.1.173 // indirect
	github.com/iij/doapi v0.0.0-20190504054126-0bbf12d6d7df // indirect
	github.com/in-toto/in-toto-golang v0.5.0 // indirect
	github.com/infobloxopen/infoblox-go-client/v2 v2.10.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.13-0.20220915233716-71ac16282d12 // indirect
	github.com/k0kubun/go-ansi v0.0.0-20180517002512-3bf9e2903213 // indirect
	github.com/klauspost/compress v1.18.0
	github.com/kolo/xmlrpc v0.0.0-20220921171641-a4b6fa1dd06b // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/labbsr0x/bindman-dns-webhook v1.0.2 // indirect
	github.com/labbsr0x/goh v1.0.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/linode/linodego v1.60.0 // indirect
	github.com/liquidweb/liquidweb-cli v0.6.9 // indirect
	github.com/liquidweb/liquidweb-go v1.6.4 // indirect
	github.com/lrstanley/bubblezone v0.0.0-20240914071701-b48c55a5e78e // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mimuret/golang-iij-dpf v0.9.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/locker v1.0.1 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/signal v0.7.1 // indirect
	github.com/moby/sys/user v0.3.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/namedotcom/go/v4 v4.0.2 // indirect
	github.com/nrdcg/auroradns v1.1.0 // indirect
	github.com/nrdcg/bunny-go v0.1.0 // indirect
	github.com/nrdcg/desec v0.11.1 // indirect
	github.com/nrdcg/dnspod-go v0.4.0 // indirect
	github.com/nrdcg/freemyip v0.3.0 // indirect
	github.com/nrdcg/goacmedns v0.2.0 // indirect
	github.com/nrdcg/goinwx v0.11.0 // indirect
	github.com/nrdcg/mailinabox v0.3.0 // indirect
	github.com/nrdcg/namesilo v0.5.0 // indirect
	github.com/nrdcg/nodion v0.1.0 // indirect
	github.com/nrdcg/oci-go-sdk/common/v1065 v1065.103.0 // indirect
	github.com/nrdcg/oci-go-sdk/dns/v1065 v1065.103.0 // indirect
	github.com/nrdcg/porkbun v0.4.0 // indirect
	github.com/nrdcg/vegadns v0.3.0 // indirect
	github.com/nzdjb/go-metaname v1.0.0 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107090550-2e043c6bd626 // indirect
	github.com/opencontainers/selinux v1.13.0 // indirect
	github.com/ovh/go-ovh v1.9.0 // indirect
	github.com/peterhellberg/link v1.2.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pquerna/otp v1.5.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/regfish/regfish-dnsapi-go v0.1.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sacloud/api-client-go v0.3.3 // indirect
	github.com/sacloud/go-http v0.1.9 // indirect
	github.com/sacloud/iaas-api-go v1.20.0 // indirect
	github.com/sacloud/packages-go v0.0.11 // indirect
	github.com/safchain/ethtool v0.5.9 // indirect
	github.com/sagikazarmark/locafero v0.4.0 // indirect
	github.com/sagikazarmark/slog-shim v0.1.0 // indirect
	github.com/scaleway/scaleway-sdk-go v1.0.0-beta.35 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.4.0 // indirect
	github.com/selectel/domains-go v1.1.0 // indirect
	github.com/selectel/go-selvpcclient/v4 v4.1.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/smartystreets/go-aws-auth v0.0.0-20180515143844-0c1422d1fdb9 // indirect
	github.com/softlayer/softlayer-go v1.2.1 // indirect
	github.com/softlayer/xmlrpc v0.0.0-20200409220501-5f089df7cb7e // indirect
	github.com/sony/gobreaker v1.0.0 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.11.0 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	github.com/spf13/viper v1.18.2 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.1.48 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/transip/gotransip/v6 v6.26.1 // indirect
	github.com/ultradns/ultradns-go-sdk v1.8.1-20250722213956-faef419 // indirect
	github.com/vbatts/tar-split v0.11.6 // indirect
	github.com/vinyldns/go-vinyldns v0.9.16 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/volcengine/volc-sdk-golang v1.0.224 // indirect
	github.com/vultr/govultr/v3 v3.24.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yandex-cloud/go-genproto v0.34.0 // indirect
	github.com/yandex-cloud/go-sdk/services/dns v0.0.16 // indirect
	github.com/yandex-cloud/go-sdk/v2 v2.24.0 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.11 // indirect
	go.mongodb.org/mongo-driver v1.13.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.56.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/ratelimit v0.3.1 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20230429144221-925a1e7659e6 // indirect
	google.golang.org/api v0.280.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/ns1/ns1-go.v2 v2.15.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gotest.tools/v3 v3.5.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
	tags.cncf.io/container-device-interface v0.8.0 // indirect
	tags.cncf.io/container-device-interface/specs-go v0.8.0 // indirect
)

// Temporary fork carrying the etcd subnet-watch compaction-recovery fix:
// upstream hot-loops forever once a watch's start revision is compacted
// away. Drop this once the fix is merged upstream into flannel-io/flannel
// and released. Tracking: MIR-1274.
replace github.com/flannel-io/flannel => github.com/mirendev/flannel v0.26.8-0.20260630195747-affa754a04b8
