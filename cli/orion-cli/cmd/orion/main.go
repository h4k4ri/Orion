package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/horizon/orion/cli/orion-cli/internal/cli"
)

func main() {
	client := cli.NewClientFromEnv()
	var cmd cli.CLI
	ctx := kong.Parse(&cmd,
		kong.Name("orion"),
		kong.Description("Orion cloud platform CLI."),
		kong.UsageOnError(),
	)
	if err := ctx.Run(client); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
