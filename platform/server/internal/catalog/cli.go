package catalog

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

func RunCLI(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "configs/service-catalog.yaml", "service catalog path")
	services := flags.String("services", "", "comma-separated service names")
	environment := flags.String("environment", "dev", "delivery environment")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*services) == "" {
		_, _ = fmt.Fprintln(stderr, "--services is required")
		return 2
	}
	data, err := Load(*catalogPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := Export(data, strings.Split(*services, ","), *environment)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
