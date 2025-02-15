package commands

import "github.com/mitchellh/cli"

type section struct {
	name string
	desc string
}

var _ cli.Command = &section{}

func Section(name, desc string) cli.Command {
	return &section{name: name, desc: desc}
}

func (s *section) Help() string {
	return s.desc
}

func (s *section) Synopsis() string {
	return s.desc
}

func (s *section) Run(args []string) int {
	return cli.RunResultHelp
}
