package schema

import (
	"bytes"
	"compress/gzip"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/entity"
)

type ecRegEntry struct {
	schema  *entity.EncodedDomain
	encoded []byte
}

var encodedRegistry = make(map[string]map[string]ecRegEntry)

func RegisterEncodedSchema(domain, version string, data []byte) {
	if _, ok := encodedRegistry[domain]; !ok {
		encodedRegistry[domain] = make(map[string]ecRegEntry)
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}

	var schema entity.EncodedDomain

	err = cbor.NewDecoder(gr).Decode(&schema)
	if err != nil {
		panic(err)
	}

	encodedRegistry[domain][version] = ecRegEntry{
		schema:  &schema,
		encoded: data,
	}
}
