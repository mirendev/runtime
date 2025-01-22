package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/jessevdk/go-flags"
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

func (w *Cmd) Help() string {
	var buf bytes.Buffer
	w.parser.WriteHelp(&buf)
	return buf.String()
}

func (w *Cmd) Synopsis() string {
	return w.syn
}

func (w *Cmd) Run(args []string) int {
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
