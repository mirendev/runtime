package commands

import (
	"strings"

	"miren.dev/mflags"
)

type section struct {
	name string
	help string
	desc string
	fs   *mflags.FlagSet
}

var _ mflags.Command = &section{}

func Section(name, desc, help string) mflags.Command {
	help = strings.TrimSpace(help)

	if help == "" {
		help = desc
	}

	if desc == "" {
		desc = help
	}

	return &section{
		name: name,
		desc: desc,
		help: help,
		fs:   mflags.NewFlagSet(name),
	}
}

func (s *section) FlagSet() *mflags.FlagSet {
	return s.fs
}

func (s *section) Run(fs *mflags.FlagSet, args []string) error {
	// Return ErrShowHelp to signal the dispatcher to show help with sub-commands
	return mflags.ErrShowHelp
}

func (s *section) Usage() string {
	return s.desc
}

// Help returns the help text
func (s *section) Help() string {
	return s.help
}

// Synopsis returns a short description
func (s *section) Synopsis() string {
	return s.desc
}
