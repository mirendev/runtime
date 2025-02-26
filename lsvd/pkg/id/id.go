package id

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"

	"github.com/mr-tron/base58"
)

// TODO add Id.Type

type Id [16]byte

func Generate() (Id, error) {
	var id Id

	_, err := io.ReadFull(rand.Reader, id[:])
	if err != nil {
		return id, err
	}

	return id, nil
}

func (id Id) Valid() bool {
	for _, b := range id {
		if b != 0 {
			return true
		}
	}

	return false
}

func (id Id) Validate() error {
	for _, b := range id {
		if b != 0 {
			return nil
		}
	}

	return fmt.Errorf("id is all zeros")
}

func (id Id) String() string {
	return base58.Encode(id[:])
}

func (id Id) Mac() [6]byte {
	var ret [6]byte

	copy(ret[:], id[:6])

	ret[0] = (ret[0] & 0xf0) | 0x02

	return ret
}

func (id Id) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *Id) UnmarshalText(data []byte) error {
	i, err := base58.Decode(string(data))
	if err != nil {
		return err
	}

	copy((*id)[:], i)
	return nil
}

func (id *Id) UnmarshalFlag(value string) error {
	i, err := base58.Decode(value)
	if err != nil {
		return err
	}

	copy((*id)[:], i)
	return nil

}

func (id Id) MarshalFlag() (string, error) {
	return base58.Encode(id[:]), nil
}

func (id Id) MacString() string {
	mac := id.Mac()
	return net.HardwareAddr(mac[:]).String()
}
