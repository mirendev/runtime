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

	"github.com/jessevdk/go-flags"
	toml "github.com/pelletier/go-toml/v2"
)

type Cmd struct {
	syn, name string
	f         reflect.Value

	opts   reflect.Value
	global *GlobalFlags
	parser *flags.Parser
}

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

	parser := flags.NewNamedParser(name, flags.HelpFlag|flags.PassDoubleDash)
	parser.ShortDescription = syn
	parser.LongDescription = syn

	var globalFlags GlobalFlags
	_, err := parser.AddGroup("Global Options", "", &globalFlags)
	if err != nil {
		panic(err)
	}

	_, err = parser.AddGroup("Command Options", "", sv.Interface())
	if err != nil {
		panic(err)
	}

	return &Cmd{
		syn:    syn,
		name:   name,
		f:      rv,
		global: &globalFlags,
		opts:   sv,
		parser: parser,
	}
}

func (w *Cmd) ReadConfig(path string) error {
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
			field.SetString(ExpandPath(field.String()))
		case "address":
			val := field.String()
			_, _, err := net.SplitHostPort(val)
			if err != nil {
				return fmt.Errorf("error validating %s as address: %s", name, err)
			}
		}
	}

	return nil
}

func (w *Cmd) Help() string {
	var buf bytes.Buffer
	w.parser.WriteHelp(&buf)
	return buf.String()
}

func (w *Cmd) Synopsis() string {
	return w.syn
}

func (w *Cmd) loadConfig(args []string) error {
	for i, arg := range args {
		switch {
		case arg == "--config":
			if i+1 < len(args) {
				return w.ReadConfig(args[i+1])
			} else {
				return fmt.Errorf("missing argument for --config")
			}
		case strings.HasPrefix(arg, "--config="):
			return w.ReadConfig(arg[9:])
		}
	}

	return nil
}

func (w *Cmd) Run(args []string) int {
	if err := w.loadConfig(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	_, err := w.parser.ParseArgs(args)
	if err != nil {
		flagsErr, ok := err.(*flags.Error)

		if ok && flagsErr.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stdout, err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}

		return 1
	}

	err = w.clean(reflect.ValueOf(w.global).Elem())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	err = w.clean(w.opts.Elem())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
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
			fmt.Fprintf(os.Stderr, "An error occured:\n%s\n", err)
			return 1
		}
	}

	return ctx.exitCode
}

type CommandOutput struct {
	Stderr bytes.Buffer
	Stdout bytes.Buffer
}

func RunCommand(f any, args ...string) (*CommandOutput, error) {
	cmd := Infer("test command", "A command being tested", f)

	var out CommandOutput

	_, err := cmd.parser.ParseArgs(args)
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
