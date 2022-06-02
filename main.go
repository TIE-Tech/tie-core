package main

import (
	"fmt"
	"os"

	"github.com/mitchellh/cli"
	"github.com/tie-core/cmd/util"
)

func main() {
	os.Exit(Run(os.Args[1:]))
}

// Run starts the cli
func Run(args []string) int {
	commands := util.Commands()

	cli := &cli.CLI{
		Name:     "tie",
		Args:     args,
		Commands: commands,
	}

	exitCode, err := cli.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing CLI: %s\n", err.Error())

		return 1
	}
	return exitCode
}