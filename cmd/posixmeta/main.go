package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/utkuozdemir/pv-migrate/internal/posixmeta"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "posixmeta: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: posixmeta <save|apply> [flags] <dir> <manifest.gz>")
	}

	switch args[0] {
	case "save":
		return runSave(args[1:])
	case "apply":
		return runApply(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runSave(args []string) error {
	flags := flag.NewFlagSet("save", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	mountRoot := flags.String("mount-root", "", "volume mount root enclosing source directory")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 2 {
		return errors.New("usage: posixmeta save [--mount-root <dir>] <dir> <manifest.gz>")
	}

	return posixmeta.Save(flags.Arg(0), flags.Arg(1), posixmeta.SaveOptions{
		MountRoot: *mountRoot,
	})
}

func runApply(args []string) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	noChown := flags.Bool("no-chown", false, "omit ownership changes")
	nonRoot := flags.Bool("non-root", false, "skip chown and directory mtimes")
	mountRoot := flags.String("mount-root", "", "volume mount root enclosing target directory")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 2 {
		return errors.New("usage: posixmeta apply [--no-chown] [--non-root] [--mount-root <dir>] <dir> <manifest.gz>")
	}

	return posixmeta.Apply(flags.Arg(0), flags.Arg(1), posixmeta.ApplyOptions{
		MountRoot: *mountRoot,
		NoChown:   *noChown,
		NonRoot:   *nonRoot,
	})
}
