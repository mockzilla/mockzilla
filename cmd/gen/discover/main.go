package main

import (
	"flag"
	"fmt"
	"os"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
)

func main() {
	flag.Parse()

	// Get services directory from positional argument
	servicesDir := ""
	if flag.NArg() > 0 {
		servicesDir = flag.Arg(0)
	}

	err := cmdapi.Discover(cmdapi.DiscoverOptions{
		ServicesDir: servicesDir,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
