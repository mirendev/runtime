package commands

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"miren.dev/mflags"
)

// Cmd wraps a command function with mflags parsing
type Cmd struct {
	syn, name string
	f         reflect.Value

	opts   reflect.Value
	global *GlobalFlags
	fs     *mflags.FlagSet
}

// Infer creates a command from a function with the signature:
// func(ctx *Context, opts StructType) error
func Infer(name, syn string, f interface{}) *Cmd {
	rv := reflect.ValueOf(f)

	if rv.Kind() != reflect.Func {
		panic("must pass a function")
	}

	rt := rv.Type()

	if rt.NumIn() != 2 {
		panic("must provide two arguments only")
	}

	if rt.NumOut() != 1 {
		panic("must return one argument only")
	}

	if rt.In(0) != reflect.TypeFor[*Context]() {
		panic("first argument must be *Context")
	}

	in := rt.In(1)

	if in.Kind() != reflect.Struct {
		panic("argument must be a struct")
	}

	sv := reflect.New(in)

	fs := mflags.NewFlagSet(name)

	var globalFlags GlobalFlags

	// Parse global flags from struct
	err := fs.FromStruct(&globalFlags)
	if err != nil {
		panic(fmt.Sprintf("error parsing global flags: %v", err))
	}

	// Parse command options from struct
	err = fs.FromStruct(sv.Interface())
	if err != nil {
		panic(fmt.Sprintf("error parsing command options: %v", err))
	}

	return &Cmd{
		syn:    syn,
		name:   name,
		f:      rv,
		global: &globalFlags,
		opts:   sv,
		fs:     fs,
	}
}

// FlagSet implements mflags.Command
func (w *Cmd) FlagSet() *mflags.FlagSet {
	return w.fs
}

// Usage implements mflags.Command
func (w *Cmd) Usage() string {
	return w.syn
}

func (w *Cmd) ReadOptions(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer f.Close()

	vals := make(map[string]any)

	dec := toml.NewDecoder(f)
	err = dec.Decode(&vals)
	if err != nil {
		return err
	}

	err = w.consumeValues(w.opts.Elem(), vals)
	if err != nil {
		return err
	}

	err = w.consumeValues(reflect.ValueOf(w.global).Elem(), vals)
	if err != nil {
		return err
	}

	return err
}

func (w *Cmd) consumeValues(rv reflect.Value, vals map[string]any) error {
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)

		ft := rv.Type().Field(i)
		name := ft.Tag.Get("long")

		if val, ok := vals[name]; ok {
			vv := reflect.ValueOf(val)
			if vv.Kind() == field.Kind() {
				field.Set(vv.Convert(ft.Type))
			}
		}
	}

	return nil
}

func (w *Cmd) show(rv reflect.Value) {
	vals := make(map[string]any)

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		name := rv.Type().Field(i).Tag.Get("long")
		if name == "" {
			name = rv.Type().Field(i).Tag.Get("short")
		}
		vals[name] = field.Interface()
	}

	data, err := toml.Marshal(vals)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Print(string(data))
	}
}

func (w *Cmd) clean(rv reflect.Value) error {
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		typ := rv.Type().Field(i).Tag.Get("type")
		if typ == "" {
			continue
		}

		name := rv.Type().Field(i).Tag.Get("long")
		if name == "" {
			name = rv.Type().Field(i).Tag.Get("short")
		}

		switch typ {
		case "path":
			path := ExpandPath(field.String())
			if _, err := os.Stat(path); err != nil {
				err = os.MkdirAll(path, 0700)
				if err != nil {
					return fmt.Errorf("error validating %s as path: %w", name, err)
				}
			}

			field.SetString(path)
		case "address":
			val := field.String()
			if val == "" {
				continue
			}
			_, _, err := net.SplitHostPort(val)
			if err != nil {
				return fmt.Errorf("error validating %s as address: %s", name, err)
			}
		}
	}

	return nil
}

// Help returns help text for the command
func (w *Cmd) Help() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Usage: %s [options]\n\n", w.name)
	fmt.Fprintf(&buf, "%s\n\n", w.syn)
	fmt.Fprintf(&buf, "Options:\n")
	w.fs.VisitAll(func(f *mflags.Flag) {
		if f.Short != 0 {
			fmt.Fprintf(&buf, "  -%c, --%s\n", f.Short, f.Name)
		} else {
			fmt.Fprintf(&buf, "      --%s\n", f.Name)
		}
		fmt.Fprintf(&buf, "        %s", f.Usage)
		if f.DefValue != "" {
			fmt.Fprintf(&buf, " (default: %s)", f.DefValue)
		}
		fmt.Fprintf(&buf, "\n")
	})
	return buf.String()
}

// Synopsis returns a short description
func (w *Cmd) Synopsis() string {
	return w.syn
}

func (w *Cmd) loadOptions(args []string) error {
	for i, arg := range args {
		switch {
		case arg == "--options":
			if i+1 < len(args) {
				return w.ReadOptions(args[i+1])
			} else {
				return fmt.Errorf("missing argument for --options")
			}
		case strings.HasPrefix(arg, "--options="):
			return w.ReadOptions(arg[10:])
		}
	}

	return nil
}

type OptsValidate interface {
	Validate(glbl *GlobalFlags) error
}

// Run implements mflags.Command
func (w *Cmd) Run(fs *mflags.FlagSet, args []string) error {
	return w.Invoke(args...)
}

func (w *Cmd) Invoke(args ...string) error {
	if err := w.loadOptions(args); err != nil {
		return fmt.Errorf("error loading options: %w", err)
	}

	err := w.clean(reflect.ValueOf(w.global).Elem())
	if err != nil {
		return fmt.Errorf("error cleaning global options: %w", err)
	}

	err = w.clean(w.opts.Elem())
	if err != nil {
		return fmt.Errorf("error cleaning command options: %w", err)
	}

	if ov, ok := w.opts.Interface().(OptsValidate); ok {
		err = ov.Validate(w.global)
		if err != nil {
			return fmt.Errorf("error validating options: %w", err)
		}
	}

	if os.Getenv("DEBUG_CONFIG") != "" {
		fmt.Println("# Configuration")
		w.show(reflect.ValueOf(w.global).Elem())
		w.show(w.opts.Elem())
	}

	ctx := setup(context.Background(), w.global, w.opts.Interface())
	defer ctx.Close()

	rets := w.f.Call([]reflect.Value{reflect.ValueOf(ctx), w.opts.Elem()})

	if err, ok := rets[0].Interface().(error); ok {
		if err != nil {
			return err
		}
	}

	if ctx.exitCode != 0 {
		return ErrExitCode(ctx.exitCode)
	}

	return nil
}

type ErrExitCode int

func (e ErrExitCode) Error() string {
	return fmt.Sprintf("exit code %d", e)
}

type CommandOutput struct {
	Stderr bytes.Buffer
	Stdout bytes.Buffer
}

func RunCommand(f any, args ...string) (*CommandOutput, error) {
	cmd := Infer("test command", "A command being tested", f)

	var out CommandOutput

	err := cmd.fs.Parse(args)
	if err != nil {
		out.Stderr.WriteString(err.Error())
		return &out, err
	}

	ctx := setup(context.Background(), cmd.global, cmd.opts.Interface())
	defer ctx.Close()

	ctx.Stdout = &out.Stdout
	ctx.Stderr = &out.Stderr

	rets := cmd.f.Call([]reflect.Value{reflect.ValueOf(ctx), cmd.opts.Elem()})

	if err, ok := rets[0].Interface().(error); ok {
		if err != nil {
			return &out, err
		}
	}

	return &out, nil
}

func ExpandPath(path string) string {
	if path == "" {
		return ""
	}

	if strings.HasPrefix(path, "~/") {
		return os.ExpandEnv("$HOME" + path[1:])
	}

	dir, err := filepath.Abs(path)
	if err != nil {
		// only happens if getwd fails, which means everything is broken.
		panic(err)
	}

	return dir
}
