package clusternetwork

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportAdvertisingNothingSendsEmptyDetails(t *testing.T) {
	// Present-but-empty is how cloud tells "advertising nothing" from a
	// runtime that predates details, which sends no field at all.
	b, err := json.Marshal(Report{APIAddressDetails: []AdvertisedAddress{}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"api_address_details":[],"containerized":false}`, string(b))
}

func TestReportDetailsEncoding(t *testing.T) {
	b, err := json.Marshal(Report{
		APIAddresses: []string{"100.107.209.9:8443"},
		APIAddressDetails: []AdvertisedAddress{{
			Transport: TransportIP,
			Address:   "100.107.209.9:8443",
			Class:     ClassOverlay,
			Range:     RangeShared,
			Interface: "tailscale0",
			Source:    SourceDiscovered,
			Label:     "tailscale",
		}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"api_addresses": ["100.107.209.9:8443"],
		"api_address_details": [{
			"transport": "ip",
			"address": "100.107.209.9:8443",
			"class": "overlay",
			"range": "shared",
			"interface": "tailscale0",
			"source": "discovered",
			"label": "tailscale"
		}],
		"containerized": false
	}`, string(b))
}
