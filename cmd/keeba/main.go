// Command keeba bootstraps an AI-native wiki: schema discipline, drift
// detection, MCP integration, ingest agents.
package main

import (
	"fmt"
	"os"

	"github.com/aomerk/keeba/internal/cli"
)

func main() {
	err := cli.NewRoot().Execute()
	if err == nil {
		return
	}
	if cli.IsSilentExit(err) {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
