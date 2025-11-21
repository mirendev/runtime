package commands

import (
	"strings"

	"github.com/mitchellh/cli"
)

type section struct {
	name string
	help string
	desc string
}

var _ cli.Command = &section{}

func Section(name, desc, help string) cli.Command {
	help = strings.TrimSpace(help)

	if help == "" {
		help = desc
	}

	if desc == "" {
		desc = help
	}

	return &section{name: name, desc: desc}
}

func (s *section) Help() string {
	return s.help
}

func (s *section) Synopsis() string {
	return s.desc
}

func (s *section) Run(args []string) int {
	return cli.RunResultHelp
}
