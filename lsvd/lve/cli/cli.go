package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"

	"github.com/lab47/cleo"
	"github.com/mitchellh/cli"
	"golang.org/x/term"
	"miren.dev/runtime/lsvd/lve/coreinst"
	"miren.dev/runtime/lsvd/lve/gatewaymgr"
	"miren.dev/runtime/lsvd/lve/paths"
	"miren.dev/runtime/lsvd/lve/pkg/id"
	"miren.dev/runtime/lsvd/lve/userinst"
)

type CLI struct {
	log *slog.Logger

	lc *cli.CLI
}

type Global struct {
	Debug bool `short:"D" long:"debug" description:"enabel debug mode"`
}

func NewCLI(log *slog.Logger, args []string) (*CLI, error) {
	c := &CLI{
		log: log,
		lc:  cli.NewCLI("lsvd", "alpha"),
	}

	c.lc.Args = args

	err := c.setupCommands()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *CLI) Run() (int, error) {
	return c.lc.Run()
}

func (c *CLI) setupCommands() error {
	c.lc.Commands = map[string]cli.CommandFactory{
		"gw create": func() (cli.Command, error) {
			return cleo.Infer("gw create", "create a new gateway", c.gwCreate), nil
		},
		"gw run": func() (cli.Command, error) {
			return cleo.Infer("gw run", "run a gateway instance", c.gwRun), nil
		},
		"inf generate": func() (cli.Command, error) {
			return cleo.Infer("inf generate", "generate a new interface id", c.infGenerate), nil
		},
		"console": func() (cli.Command, error) {
			return cleo.Infer("console", "connect to a local console", c.consoleConnect), nil
		},
		"user run": func() (cli.Command, error) {
			return cleo.Infer("user run", "run a user instance", c.userRun), nil
		},
	}

	return nil
}

func (c *CLI) gwCreate(ctx context.Context, opts struct {
	Global
	Config string `short:"c" long:"config" description:"gateway configuration"`
	Path   string `short:"p" long:"path" description:"path for gateway setup"`
}) error {
	log := c.log

	f, err := os.Open(opts.Config)
	if err != nil {
		return err
	}

	var cfg gatewaymgr.Config

	err = json.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return err
	}

	return gatewaymgr.Create(log, opts.Path, &cfg)
}

func (c *CLI) gwRun(ctx context.Context, opts struct {
	Global
	Config string `short:"c" long:"config" description:"gateway configuration"`
	Path   string `short:"p" long:"path" description:"path for gateway setup"`
}) error {
	log := c.log

	f, err := os.Open(opts.Config)
	if err != nil {
		return err
	}

	var cfg coreinst.CoreConfig

	err = json.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return err
	}

	return coreinst.BootCoreInst(ctx, log, &cfg, opts.Path)
}

func (c *CLI) userRun(ctx context.Context, opts struct {
	Global
	Config   string `short:"c" long:"config" description:"gateway configuration"`
	Path     string `short:"p" long:"path" description:"path for gateway setup"`
	UserData string `short:"u" long:"user-data" description:"data to expose as user-data"`
}) error {
	log := c.log

	f, err := os.Open(opts.Config)
	if err != nil {
		return err
	}

	var cfg userinst.UserConfig

	err = json.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return err
	}

	if opts.UserData != "" {
		data, err := os.ReadFile(opts.UserData)
		if err != nil {
			return err
		}

		cfg.UserData = string(data)
	}

	return userinst.BootInst(ctx, log, &cfg, opts.Path)
}

func (c *CLI) infGenerate(ctx context.Context, opts struct {
	Global
}) error {
	id, err := id.Generate()
	if err != nil {
		return err
	}

	fmt.Println(id)

	return nil
}

func (c *CLI) consoleConnect(ctx context.Context, opts struct {
	Global
	Id   id.Id  `short:"i" long:"id" description:"instance id to connect to"`
	Path string `short:"p" long:"path" description:"root path for install"`
}) error {
	log := c.log

	root := paths.Root(opts.Path)

	consolePath := root.InstanceConsole(opts.Id)
	if _, err := os.Stat(consolePath); err != nil {
		return fmt.Errorf("unable to find instance console: %s", consolePath)
	}

	conn, err := net.Dial("unix", consolePath)
	if err != nil {
		return err
	}

	log.Info("connected to console", "id", opts.Id)

	st, err := term.MakeRaw(int(os.Stdout.Fd()))
	if err != nil {
		return err
	}

	defer term.Restore(int(os.Stdout.Fd()), st)

	go io.Copy(conn, os.Stdin)
	io.Copy(os.Stdout, conn)

	return nil
}
