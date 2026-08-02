package releasestore

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

type cliOptions struct {
	databaseURL    string
	service        string
	environment    string
	record         bool
	stableDigest   bool
	fail           bool
	status         string
	gitRevision    string
	imageDigest    string
	configRevision string
	rolloutStrategy string
}

// RunCLI implements `platform-server release-record`.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("release-record", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts cliOptions
	flags.StringVar(&opts.databaseURL, "database-url", "", "PostgreSQL connection string (defaults to DATABASE_URL or PG* env)")
	flags.StringVar(&opts.service, "service", "", "delivery service name")
	flags.StringVar(&opts.environment, "environment", "dev", "delivery environment")
	flags.BoolVar(&opts.record, "record", false, "record a release state transition")
	flags.BoolVar(&opts.stableDigest, "stable-digest", false, "query the verified stable version")
	flags.BoolVar(&opts.fail, "fail", false, "record a failed release (shorthand for --record --status failed)")
	flags.StringVar(&opts.status, "status", "", "release status: releasing, stable, failed, compensating")
	flags.StringVar(&opts.gitRevision, "git-revision", "", "source repository git revision")
	flags.StringVar(&opts.imageDigest, "image-digest", "", "image digest (sha256:...)")
	flags.StringVar(&opts.configRevision, "config-revision", "", "GitOps repository revision that published the digest")
	flags.StringVar(&opts.rolloutStrategy, "rollout-strategy", "", "canary, bluegreen or rolling")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	modes := 0
	for _, enabled := range []bool{opts.record, opts.stableDigest, opts.fail} {
		if enabled {
			modes++
		}
	}
	if modes != 1 {
		_, _ = fmt.Fprintln(stderr, "provide exactly one of --record, --stable-digest or --fail")
		return 2
	}
	if strings.TrimSpace(opts.service) == "" {
		_, _ = fmt.Fprintln(stderr, "--service is required")
		return 2
	}

	ctx := context.Background()
	connString := ResolveConnString(opts.databaseURL)
	if connString == "" {
		_, _ = fmt.Fprintln(stderr, "no database connection configured (set --database-url or DATABASE_URL)")
		return 2
	}

	switch {
	case opts.stableDigest:
		record, err := StableDigest(ctx, connString, opts.service, opts.environment)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(record); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case opts.fail:
		opts.status = StatusFailed
		opts.record = true
	}

	record := ReleaseRecord{
		Service:         opts.service,
		Environment:     opts.environment,
		GitRevision:     opts.gitRevision,
		ImageDigest:     opts.imageDigest,
		ConfigRevision:  opts.configRevision,
		RolloutStrategy: opts.rolloutStrategy,
		ReleaseStatus:   opts.status,
	}
	if err := Record(ctx, connString, record); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "recorded %s %s as %s\n", opts.service, opts.environment, opts.status)
	return 0
}
