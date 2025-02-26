package server

import (
	"miren.dev/runtime/addons/mysql"
	"miren.dev/runtime/addons/postgres"
	"miren.dev/runtime/addons/redis"
)

func (s *Server) SetupBuiltinAddons() error {
	var (
		pg postgres.Addon
		my mysql.Addon
		rd redis.Addon
	)

	err := s.Reg.Populate(&pg)
	if err != nil {
		return err
	}

	err = s.Reg.Populate(&my)
	if err != nil {
		return err
	}

	err = s.Reg.Populate(&rd)
	if err != nil {
		return err
	}

	s.AddonReg.Register("postgres", &pg)
	s.AddonReg.Register("mysql", &my)
	s.AddonReg.Register("redis", &rd)

	return nil
}
