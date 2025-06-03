package commands

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/appconfig"
)

func Import(ctx *Context, opts struct {
	ConfigCentric

	Force bool `short:"f" long:"force" description:"Force import"`

	Env []string `short:"e" long:"env" description:"Override environment variables"`

	DryRun bool `short:"n" long:"dry-run" description:"Dry run"`

	NoDeploy bool `long:"no-deploy" description:"Do not deploy the app, configure only"`

	NoDefault bool `long:"no-default" description:"Don't automatically set the app as the default app if possible"`

	Args struct {
		Source string `positional-arg-name:"source" required:"true"`
	} `positional-args:"yes"`
}) error {
	var (
		dir string
		err error
	)

	if opts.Args.Source == "" {
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	} else if st, err := os.Stat(opts.Args.Source); err == nil && st.IsDir() {
		dir = opts.Args.Source
	} else if strings.HasPrefix(opts.Args.Source, "github.com") {
		ctx.Info("Cloning %s", opts.Args.Source)

		parts := strings.Split(opts.Args.Source, "/")

		if len(parts) < 3 {
			return fmt.Errorf("invalid source: %s", opts.Args.Source)
		}

		repo := parts[1] + "/" + parts[2]

		rdir := strings.Join(parts[3:], "/")

		gdir := filepath.Base(repo)

		if _, err := os.Stat(gdir); err == nil {
			return fmt.Errorf("directory %s already exists", gdir)
		}

		cmd := exec.CommandContext(ctx, "git", "clone", "https://github.com/"+repo, gdir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		err = cmd.Run()
		if err != nil {
			return err
		}

		dir = filepath.Join(gdir, rdir)
	} else {
		return fmt.Errorf("invalid source: %s", opts.Args.Source)
	}

	ctx.Begin("Importing %s", dir)

	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		return err
	}

	if ac == nil {
		return fmt.Errorf("no app config found")
	}

	ctx.Info("App Name: %s", ac.Name)

	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	aclient := app_v1alpha.CrudClient{Client: cl}

	var envvars []*app_v1alpha.NamedValue

	ctx.Begin("Configuring environment variables")

	if !opts.Force {
		_, err := aclient.GetConfiguration(ctx, ac.Name)
		if err == nil {
			return fmt.Errorf("app %s already exists", ac.Name)
		}
	}

	known := make(map[string]bool)

	for _, v := range opts.Env {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env var: %s", v)
		}

		idx := slices.IndexFunc(envvars, func(nv *app_v1alpha.NamedValue) bool {
			return nv.Key() == parts[0]
		})

		if idx == -1 {
			var nv app_v1alpha.NamedValue

			nv.SetKey(parts[0])
			nv.SetValue(parts[1])

			envvars = append(envvars, &nv)
		} else if envvars[idx].Value() != parts[1] {
			envvars[idx].SetValue(parts[1])
		}

		known[parts[0]] = true
	}

	for _, env := range ac.EnvVars {
		if known[env.Name] {
			continue
		}

		ctx.Info("Setting %s...", env.Name)

		var nv app_v1alpha.NamedValue
		nv.SetKey(env.Name)

		switch env.Generator {
		case "":
			nv.SetValue(env.Value)
		case "random-secret":
			data := make([]byte, 32)
			_, err := rand.Read(data)
			if err != nil {
				return err
			}

			nv.SetValue(hex.EncodeToString(data))
			nv.SetSensitive(true)
		default:
			return fmt.Errorf("unknown generator: %s", env.Generator)
		}

		envvars = append(envvars, &nv)
	}

	var newCfg app_v1alpha.Configuration
	newCfg.SetEnvVars(envvars)

	var wc, threads int

	var autoCon *app_v1alpha.AutoConcurrency

	for _, env := range envvars {
		switch env.Key() {
		case "WEB_CONCURRENCY":
			if env.Value() == "auto" {
				autoCon = &app_v1alpha.AutoConcurrency{}
				continue
			}
			v, err := strconv.Atoi(env.Value())
			if err != nil {
				return fmt.Errorf("invalid WEB_CONCURRENCY: %s", env.Value())
			}

			wc = v
		case "RAILS_MAX_THREADS", "THREADS", "PUMA_MAX_THREADS", "MAX_THREADS":
			v, err := strconv.Atoi(env.Value())
			if err != nil {
				return fmt.Errorf("invalid %s: %s", env.Key(), env.Value())
			}

			threads = v
		}
	}

	if wc != 0 {
		if threads != 0 {
			wc = wc * threads
		}
	} else if threads != 0 {
		wc = threads
	}

	if autoCon != nil {
		if wc != 0 {
			ctx.Info("Setting concurrency: auto (factor of %d)...", wc)
			autoCon.SetFactor(int32(wc))
		} else {
			ctx.Info("Setting concurrency: auto...")
		}

		newCfg.SetAutoConcurrency(autoCon)
	} else if wc > 0 {
		ctx.Info("Setting concurrency: %d...", wc)
		newCfg.SetConcurrency(int32(wc))
	}

	if opts.DryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(&newCfg)
		return nil
	}

	_, err = aclient.New(ctx, ac.Name)
	if err != nil {
		return err
	}

	setres, err := aclient.SetConfiguration(ctx, ac.Name, &newCfg)
	if err != nil {
		return err
	}

	ctx.Completed("Published new version, id: %s", setres.VersionId())

	if opts.NoDeploy {
		return nil
	}

	ctx.Begin("Deploying %s", ac.Name)

	deployCmd := Infer("deploy", "", Deploy)
	err = deployCmd.Invoke("--app", ac.Name, "--dir", dir)
	if err != nil {
		return err
	}

	if opts.NoDefault {
		return nil
	}

	results, err := aclient.List(ctx)
	if err != nil {
		return err
	}

	if len(results.Apps()) == 1 {
		_, err = aclient.SetHost(ctx, ac.Name, "*")
		if err != nil {
			return err
		}
		ctx.Completed("Setup app as default app")
	} else {
		ctx.Info("Other apps exist, not setting up as default")
	}

	return nil
}
