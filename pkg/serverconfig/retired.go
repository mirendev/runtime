package serverconfig

import (
	"errors"
	"fmt"
)

// ValidateRetiredTopology refuses configuration for the retired distributed
// server mode and its external data stores (MIR-2064). Those settings used to
// point the server at state it no longer knows how to reach, so ignoring them
// would boot a fresh, empty embedded store in place of the operator's data.
// Failing startup instead keeps that data untouched and lets an upgrade's
// readiness check roll back to the previous build.
//
// Only the settings that selected an external store are refused: a mode other
// than standalone, or start_embedded = false. The external addresses
// (etcd.endpoints, buildkit.socket_path, the Victoria addresses) were ignored
// unless one of those was set, and earlier releases' `config generate
// --defaults` wrote the Victoria ones into every config, so refusing them
// would block servers that never left the embedded stores. Values that
// already match the only supported topology (mode = "standalone",
// start_embedded = true) are accepted as no-ops.
func (c *Config) ValidateRetiredTopology() error {
	var errs []error
	retired := func(key, why string) {
		errs = append(errs, fmt.Errorf("%s is no longer supported (%s); remove it from the server config file, flags, or environment", key, why))
	}

	if mode := c.GetMode(); mode != "" && mode != "standalone" {
		retired(fmt.Sprintf("mode = %q", mode), "standalone is the only server mode")
	}
	for _, embedded := range []struct {
		key string
		val *bool
	}{
		{"etcd.start_embedded", c.Etcd.StartEmbedded},
		{"containerd.start_embedded", c.Containerd.StartEmbedded},
		{"buildkit.start_embedded", c.Buildkit.StartEmbedded},
		{"victorialogs.start_embedded", c.Victorialogs.StartEmbedded},
		{"victoriametrics.start_embedded", c.Victoriametrics.StartEmbedded},
	} {
		if embedded.val != nil && !*embedded.val {
			retired(embedded.key+" = false", "the server always runs this component itself")
		}
	}
	return errors.Join(errs...)
}
