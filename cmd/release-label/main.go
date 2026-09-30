// Command release-label prints the Content Release label of a site.yml (used by the CI release gate).
package main

import (
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: release-label <site.yml>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var site struct {
		Release struct {
			Label string `yaml:"label"`
		} `yaml:"release"`
	}
	if err := yaml.Unmarshal(raw, &site); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(site.Release.Label)
}
