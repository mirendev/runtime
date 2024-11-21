package network

import (
	"encoding/json"
	"errors"
	"net/netip"

	"go4.org/netipx"
)

type IPPool struct {
	prefix netip.Prefix
	last   netip.Addr

	router netip.Addr
	next   netip.Addr

	deallocated []netip.Addr
}

func (i *IPPool) Init(cidr string, allocRouter bool) error {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return err
	}

	i.prefix = prefix.Masked()

	i.last = netipx.PrefixLastIP(i.prefix)

	if prefix.Addr().Is6() {
		i.last = i.last.Next()
	}

	if allocRouter {
		i.router = i.prefix.Addr().Next()
		i.next = i.router.Next()
	} else {
		i.next = i.prefix.Addr()
	}
	return nil
}

func (i *IPPool) Router() netip.Prefix {
	return netip.PrefixFrom(i.router, i.prefix.Bits())
}

var ErrAddressesExhausted = errors.New("no more addresses")

func (i *IPPool) Allocate() (netip.Prefix, error) {
	addr := i.next

	if addr == i.last {
		if len(i.deallocated) > 0 {
			addr = i.deallocated[0]
			i.deallocated = i.deallocated[1:]
		} else {
			return netip.Prefix{}, ErrAddressesExhausted
		}
	}

	i.next = addr.Next()

	return netip.PrefixFrom(addr, i.prefix.Bits()), nil
}

func (i *IPPool) Deallocate(addr netip.Prefix) error {
	i.deallocated = append(i.deallocated, addr.Addr())
	return nil
}

type poolState struct {
	Prefix netip.Prefix `json:"prefix"`
	Last   netip.Addr   `json:"last"`
	Router netip.Addr   `json:"router"`
	Next   netip.Addr   `json:"next"`

	Deallocated []netip.Addr `json:"deallocated"`
}

func (i *IPPool) MarshalBinary() ([]byte, error) {
	return json.Marshal(poolState{
		Prefix:      i.prefix,
		Last:        i.last,
		Router:      i.router,
		Next:        i.next,
		Deallocated: i.deallocated,
	})
}

func (i *IPPool) UnmarshalBinary(data []byte) error {
	var ps poolState

	err := json.Unmarshal(data, &ps)
	if err != nil {
		return err
	}

	i.prefix = ps.Prefix
	i.last = ps.Last
	i.router = ps.Router
	i.next = ps.Next
	i.deallocated = ps.Deallocated

	return nil
}
