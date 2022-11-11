package main

import (
	"os"

	"github.com/spiffe/spire/cmd/spire-agent/cli"
	"github.com/spiffe/spire/cmd/spire-agent/service"
)

func main() {
	serviceRunner := service.NewRunner(os.Args[1:])
	if serviceRunner.NeedToRunAsAService() {
		os.Exit(serviceRunner.Run())
	}
	os.Exit(new(cli.CLI).Run(os.Args[1:]))
}
