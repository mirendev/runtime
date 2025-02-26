package gatewaymgr

import "miren.dev/runtime/lsvd/lve/pkg/id"

type DNSConfig struct {
	Upstream string `json:"upstream"`
}

type DHCPConfig struct {
	Router  string `json:"router"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Netmask string `json:"netmask"`
}

type RouterConfig struct {
	Address string `json:"address"`
	Netmask string `json:"netmask"`
	Gateway string `json:"gateway"`
}

type InterfaceConfig struct {
	VPC  id.Id  `json:"vpc"`
	Name string `json:"name"`
	Id   id.Id  `json:"id"`

	HostTap string `json:"host-tap"`
}

type Config struct {
	Id         id.Id              `json:"id"`
	Interfaces []*InterfaceConfig `json:"interfaces"`

	DHCP *DHCPConfig  `json:"dhcp"`
	DNS  []*DNSConfig `json:"dns"`

	Router *RouterConfig `json:"router"`
}
