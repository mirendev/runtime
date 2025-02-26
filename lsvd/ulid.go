package lsvd

import "github.com/oklog/ulid/v2"

type UlidRecall struct {
	Past []ulid.ULID
}

func (u *UlidRecall) First() ulid.ULID {
	return u.Past[0]
}

func (u *UlidRecall) Gen() ulid.ULID {
	ul := ulid.MustNew(ulid.Now(), ulid.DefaultEntropy())
	u.Past = append(u.Past, ul)
	return ul
}
