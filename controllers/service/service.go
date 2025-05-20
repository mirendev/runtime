package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/set"
)

type ServiceController struct {
	Log             *slog.Logger
	EAC             *entityserver_v1alpha.EntityAccessClient
	IPv4Routable    netip.Prefix   `asm:"ip4-routable"`
	ServicePrefixes []netip.Prefix `asm:"service-prefixes"`

	DisableLocalNet bool `asm:"disable-localnet,optional"`

	routablePrefixes []netip.Prefix

	table string
	cmd   *nftCommands

	mu             sync.Mutex
	chainEndpoints map[string][]string
}

func (s *ServiceController) UpdateEndpoints(ctx context.Context, event controller.Event) ([]entity.Attr, error) {
	var eps network_v1alpha.Endpoints
	eps.Decode(event.Entity)

	gr, err := s.EAC.Get(ctx, eps.Service.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	var srv network_v1alpha.Service
	srv.Decode(gr.Entity().Entity())

	meta := &entity.Meta{
		Entity: gr.Entity().Entity(),
	}

	s.Log.Info("Endpoint updated, triggering service update", "service", srv.ID)

	return nil, s.Create(ctx, &srv, meta)
}

type nftCommands struct {
	commands    []string
	knownChains set.Set[string]
	knownMaps   set.Set[string]
}

func (n *nftCommands) Clone() *nftCommands {
	return &nftCommands{
		knownChains: n.knownChains,
		knownMaps:   n.knownMaps,
	}
}

func (n *nftCommands) append(cmd string, args ...any) {
	n.commands = append(n.commands, fmt.Sprintf(cmd, args...))
}

func (s *ServiceController) serviceChain(ip netip.Addr, port uint16) string {
	x := blake2b.Sum256([]byte(fmt.Sprintf("%s:%d", ip.String(), port)))
	return fmt.Sprintf("service_%s", base58.Encode(x[:]))
}

func (s *ServiceController) endpointChain(ip netip.Addr, port uint16) string {
	x := blake2b.Sum256([]byte(fmt.Sprintf("%s:%d", ip.String(), port)))
	return fmt.Sprintf("endpoint_%s", base58.Encode(x[:]))
}

func (s *ServiceController) nodeportChain(ip netip.Addr, port uint16) string {
	x := blake2b.Sum256([]byte(fmt.Sprintf("%s:%d", ip.String(), port)))
	return fmt.Sprintf("nodeport_%s", base58.Encode(x[:]))
}

func (s *ServiceController) createServiceChain(cmd *nftCommands, ip netip.Addr, port int) error {
	srv := s.serviceChain(ip, uint16(port))
	if cmd.knownChains.Contains(srv) {
		return nil
	}

	cmd.knownChains.Add(srv)

	cmd.append("add chain inet %s %s", s.table, srv)
	if ip.Is4() {
		cmd.append("add element inet %s service_ip4s { %s . tcp . %d : goto %s }", s.table, ip.String(), port, srv)
	} else {
		cmd.append("add element inet %s service_ip6s { %s . tcp . %d : goto %s }", s.table, ip.String(), port, srv)
	}
	return nil
}

func (s *ServiceController) updateServiceEndpoints(cmd *nftCommands, sip netip.Addr, sport int, endpoints []string) error {
	if len(endpoints) == 0 {
		return nil
	}

	srv := s.serviceChain(sip, uint16(sport))

	slices.Sort(endpoints)

	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, ok := s.chainEndpoints[srv]; ok && slices.Equal(cur, endpoints) {
		return nil
	}

	s.chainEndpoints[srv] = endpoints

	cmd.append("flush chain inet %s %s", s.table, srv)
	var vmap []string

	for i, ep := range endpoints {
		vmap = append(vmap, fmt.Sprintf("%d : goto %s", i, ep))
	}

	cmd.append("add rule inet %s %s counter name \"services\"", s.table, srv)
	for _, rp := range s.routablePrefixes {
		if rp.Addr().Is4() {
			cmd.append("add rule inet %s %s ip saddr != %s counter jump mark-for-masq", s.table, srv, rp.String())
		} else {
			cmd.append("add rule inet %s %s ip6 saddr != %s counter jump mark-for-masq", s.table, srv, rp.String())
		}
	}
	cmd.append("add rule inet %s %s numgen random mod %d vmap { %s }", s.table, srv, len(endpoints), strings.Join(vmap, ", "))
	return nil
}

func (s *ServiceController) setupNodePort(cmd *nftCommands, nport int, sip netip.Addr, sport int) error {
	chain := s.nodeportChain(sip, uint16(sport))

	if cmd.knownChains.Contains(chain) {
		return nil
	}

	cmd.knownChains.Add(chain)

	cmd.append("add chain inet %s %s", s.table, chain)

	cmd.append("add element inet %s service_nodeports { tcp . %d : goto %s }", s.table, nport, chain)

	cmd.append("add rule inet %s %s counter name \"nodeports\"", s.table, chain)
	for _, rp := range s.routablePrefixes {
		if rp.Addr().Is4() {
			cmd.append("add rule inet %s %s ip saddr == %s goto %s", s.table, chain, rp.String(), s.serviceChain(sip, uint16(sport)))
		} else {
			cmd.append("add rule inet %s %s ip6 saddr == %s goto %s", s.table, chain, rp.String(), s.serviceChain(sip, uint16(sport)))
		}
	}

	cmd.append("add rule inet %s %s fib saddr type local counter jump mark-for-masq", s.table, chain)
	cmd.append("add rule inet %s %s fib saddr type local counter goto %s", s.table, chain, s.serviceChain(sip, uint16(sport)))
	return nil
}

func (s *ServiceController) setupEndpointChain(cmd *nftCommands, ip netip.Addr, port uint16) (string, error) {
	endpoint := s.endpointChain(ip, port)
	if cmd.knownChains.Contains(endpoint) {
		return endpoint, nil
	}

	cmd.knownChains.Add(endpoint)

	cmd.append("add chain inet %s %s", s.table, endpoint)
	if ip.Is4() {
		cmd.append("add rule inet %s %s ip saddr %s jump mark-for-masq", s.table, endpoint, ip.String())
		cmd.append("add rule inet %s %s meta l4proto tcp counter dnat ip to %s:%d", s.table, endpoint, ip.String(), port)
	} else {
		cmd.append("add rule inet %s %s ip6 saddr %s jump mark-for-masq", s.table, endpoint, ip.String())
		cmd.append("add rule inet %s %s meta l4proto tcp counter dnat ip6 to %s:%d", s.table, endpoint, ip.String(), port)
	}
	return endpoint, nil
}

func (s *ServiceController) removeEndpointChain(cmd *nftCommands, ip netip.Addr, port uint16) error {
	endpoint := s.endpointChain(ip, port)
	cmd.append("delete chain inet %s %s", s.table, endpoint)
	return nil
}

func (s *ServiceController) systemTables() ([]string, error) {
	cmd := exec.Command("nft", "-j", "list", "tables")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w (%s)", err, string(out))
	}

	var tr struct {
		Root []struct {
			Table struct {
				Name   string `json:"name"`
				Handle int    `json:"handle"`
			} `json:"table"`
		} `json:"nftables"`
	}

	if err := json.Unmarshal(out, &tr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nft output: %w", err)
	}

	var tables []string

	for _, table := range tr.Root {
		if table.Table.Name != "" {
			tables = append(tables, table.Table.Name)
		}
	}

	return tables, nil
}

func (s *ServiceController) initNFT(table string, nc *nftCommands) error {
	s.table = table

	tables, err := s.systemTables()
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}

	chains := set.New[string]()
	maps := set.New[string]()

	if slices.Contains(tables, table) {
		// TODO: assuming inet family here; read from `list tables` output instead?
		cmd := exec.Command("nft", "-j", "list", "table", "inet", table)

		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to list table %s: %w", table, err)
		}

		var result struct {
			Root []struct {
				Chain struct {
					Name   string `json:"name"`
					Table  string `json:"table"`
					Handle int    `json:"handle"`
				} `json:"chain"`

				Map struct {
					Name   string `json:"name"`
					Table  string `json:"table"`
					Handle int    `json:"handle"`
					Map    string `json:"map"`
				} `json:"map"`
			}
		}

		if err := json.Unmarshal(out, &result); err != nil {
			return fmt.Errorf("failed to unmarshal nft output: %w", err)
		}

		for _, chain := range result.Root {
			if chain.Chain.Table == table {
				chains.Add(chain.Chain.Name)
			}
			if chain.Map.Table == table {
				maps.Add(chain.Map.Name)
			}
		}
	} else {
		nc.append("add table inet %s", table)
	}

	nc.knownChains = chains
	nc.knownMaps = maps

	if !maps.Contains("service_ip4s") {
		nc.append("add map inet %s service_ip4s { type ipv4_addr . inet_proto . inet_service : verdict; }", s.table)
		maps.Add("service_ip4s")
	}

	if !maps.Contains("service_ip6s") {
		nc.append("add map inet %s service_ip6s { type ipv6_addr . inet_proto . inet_service : verdict; }", s.table)
		maps.Add("service_ip6s")
	}

	if !maps.Contains("service_nodeports") {
		nc.append("add map inet %s service_nodeports { type inet_proto . inet_service : verdict; }", s.table)
		maps.Add("service_nodeports")
	}

	if !chains.Contains("services") {
		nc.append("add chain inet %s services", s.table)
		nc.append("add counter inet %s services", s.table)
		nc.append("add counter inet %s nodeports", s.table)
		nc.append("add rule inet %s services ip daddr . meta l4proto . th dport vmap @service_ip4s", s.table)
		nc.append("add rule inet %s services ip6 daddr . meta l4proto . th dport vmap @service_ip6s", s.table)
		nc.append("add rule inet %s services meta l4proto . th dport vmap @service_nodeports", s.table)
	}

	if !chains.Contains("nat-prerouting") {
		nc.append("add chain inet %s nat-prerouting { type nat hook prerouting priority -100; }", s.table)
		nc.append("add rule inet %s nat-prerouting jump services", s.table)
	}

	if !chains.Contains("nat-output") {
		nc.append("add chain inet %s nat-output { type nat hook output priority -100; }", s.table)
		nc.append("add rule inet %s nat-output jump services", s.table)
	}

	if !chains.Contains("mark-for-masq") {
		nc.append("add chain inet %s mark-for-masq", s.table)
		nc.append("add rule inet %s mark-for-masq mark set mark or 0x2000", s.table)
	}

	if !chains.Contains("masq") {
		nc.append("add chain inet %s masq", s.table)
		nc.append("add rule inet %s masq mark and 0x2000 == 0 return", s.table)
		nc.append("add rule inet %s masq mark set mark xor 0x2000", s.table)
		nc.append("add rule inet %s masq masquerade fully-random", s.table)
	}

	if !chains.Contains("nat-postrouting") {
		nc.append("add chain inet %s nat-postrouting { type nat hook postrouting priority 100; }", s.table)
		nc.append("add rule inet %s nat-postrouting jump masq", s.table)
	}

	return nil
}

func (s *ServiceController) Init(ctx context.Context) error {
	s.chainEndpoints = make(map[string][]string)
	s.routablePrefixes = []netip.Prefix{s.IPv4Routable}

	s.Log.Info("Initializing service controller")

	if !s.DisableLocalNet {
		os.WriteFile("/proc/sys/net/ipv4/conf/all/route_localnet", []byte("1"), 0644)
	}

	cmd := &nftCommands{
		commands: []string{},
	}

	if err := s.initNFT("miren", cmd); err != nil {
		return fmt.Errorf("failed to initialize nftables: %w", err)
	}

	s.cmd = cmd

	return s.apply(ctx, cmd)
}

func (s *ServiceController) apply(ctx context.Context, cmd *nftCommands) error {
	if len(cmd.commands) == 0 {
		return nil
	}

	var buf bytes.Buffer

	for _, cmdStr := range cmd.commands {
		buf.WriteString(cmdStr)
		buf.WriteString("\n")
	}

	s.Log.Info("Applying nftables commands", "commands", buf.String())

	ecmd := exec.CommandContext(ctx, "nft", "-f", "-")
	ecmd.Stdin = &buf

	out, err := ecmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute nft command: %w (%s)", err, string(out))
	}

	return nil
}

func (s *ServiceController) Create(ctx context.Context, srv *network_v1alpha.Service, meta *entity.Meta) error {
	s.Log.Info("Creating service", "service", srv)

	if len(srv.Ip) == 0 {
		return nil
	}

	lr, err := s.EAC.List(ctx, entity.Ref(network_v1alpha.EndpointsServiceId, srv.ID))
	if err != nil {
		return fmt.Errorf("failed to list endpoints: %w", err)
	}

	tp := srv.Port[0]

	var epChains []string

	cmd := s.cmd.Clone()

	for _, ent := range lr.Values() {
		var eps network_v1alpha.Endpoints
		eps.Decode(ent.Entity())

		for _, ep := range eps.Endpoint {
			destIP, err := netip.ParseAddr(ep.Ip)
			if err != nil {
				return fmt.Errorf("failed to parse endpoint IP address: %v", err)
			}

			target := tp.TargetPort
			if target == 0 {
				target = tp.Port
			}

			ep, err := s.setupEndpointChain(cmd, destIP, uint16(target))
			if err != nil {
				return fmt.Errorf("failed to setup endpoint chain: %w", err)
			}

			epChains = append(epChains, ep)
		}
	}

	var firstIp netip.Addr

	for _, sip := range srv.Ip {
		ip, err := netip.ParseAddr(sip)
		if err != nil {
			return fmt.Errorf("failed to parse service IP address: %w", err)
		}

		if !firstIp.IsValid() {
			firstIp = ip
		}

		for _, tp := range srv.Port {
			if err := s.createServiceChain(cmd, ip, int(tp.Port)); err != nil {
				return fmt.Errorf("failed to create service chain: %w", err)
			}

			if err := s.updateServiceEndpoints(cmd, ip, int(tp.Port), epChains); err != nil {
				return fmt.Errorf("failed to update service endpoints: %w", err)
			}
		}
	}

	for _, tp := range srv.Port {
		if tp.NodePort != 0 {
			if err := s.setupNodePort(cmd, int(tp.NodePort), firstIp, int(tp.Port)); err != nil {
				return fmt.Errorf("failed to setup node port: %w", err)
			}
		}
	}

	if err := s.apply(ctx, cmd); err != nil {
		return fmt.Errorf("failed to apply nftables changes: %w", err)
	}

	return nil
}

func (s *ServiceController) Delete(ctx context.Context, id entity.Id) error {
	return nil
}
