package container

import (
	"miren.dev/runtime/pkg/entity/schema"
)

var (
	s  = schema.Builder("container", "x")
	sp = s.Builder("port")
	sm = s.Builder("mount")
	sn = s.Builder("network")
	sr = sn.Builder("route") // for network routes
)

func init() {
	s.String("image", schema.Doc("Container image"))
	s.String("name", schema.Doc("Container name"))
	s.Bool("privileged", schema.Doc("Run container in privileged mode"))
	s.String("spec", schema.Doc("Container spec"))
	s.String("command", schema.Doc("Container command"))

	s.String("log-entity", schema.Doc("Log entity for container"))
	s.String("cgroup-path", schema.Doc("Cgroup path for container"))

	s.String("label", schema.Doc("Container label"), schema.Many)
	s.String("env", schema.Doc("Container environment variables"), schema.Many)

	s.Component("port", schema.Doc("Container port"))
	sp.Int64("port", schema.Doc("Container port"))
	sp.Enum("protocol", []string{"tcp", "udp"}, schema.Doc("Container port protocol"))
	sp.String("type", schema.Doc("Container port type")) // e.g. "http", "https", "ssh"

	s.Component("mount", schema.Doc("Container mount"))
	sm.String("source", schema.Doc("Mount source path"))
	sm.String("destination", schema.Doc("Mount destination path"))

	s.Component("network", schema.Doc("Container network"))
	sn.String("address", schema.Doc("Network address"), schema.Many)
	sn.String("bridge", schema.Doc("Network bridge"))

	sr.String("destination", schema.Doc("Network route path"))
	sr.String("gateway", schema.Doc("Network route gateway"))

}
