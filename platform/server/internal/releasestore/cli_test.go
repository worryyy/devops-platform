package releasestore

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateReleaseRecord(t *testing.T) {
	valid := ReleaseRecord{
		Service: "topic", Environment: "dev", GitRevision: strings.Repeat("a", 40),
		ImageDigest: "sha256:" + strings.Repeat("0", 64), ReleaseStatus: StatusStable,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ReleaseRecord)
	}{
		{"empty service", func(r *ReleaseRecord) { r.Service = "" }},
		{"empty environment", func(r *ReleaseRecord) { r.Environment = "" }},
		{"bad status", func(r *ReleaseRecord) { r.ReleaseStatus = "deployed" }},
		{"bad digest", func(r *ReleaseRecord) { r.ImageDigest = "sha256:zzz" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			test.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", test.name)
			}
		})
	}
}

func TestRunCLIUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"--service", "topic"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected usage error, got %d", code)
	}
	if !strings.Contains(stderr.String(), "provide exactly one") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestResolveConnString(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	if got := ResolveConnString(""); got != "postgres://example" {
		t.Fatalf("DATABASE_URL not preferred: %s", got)
	}
	if got := ResolveConnString("postgres://explicit"); got != "postgres://explicit" {
		t.Fatalf("explicit URL not preferred: %s", got)
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PGHOST", "db.internal")
	t.Setenv("PGPORT", "5433")
	t.Setenv("PGUSER", "release")
	t.Setenv("PGPASSWORD", "secret")
	t.Setenv("PGDATABASE", "platform")
	got := ResolveConnString("")
	if !strings.Contains(got, "@db.internal:5433/platform?") || !strings.Contains(got, "release:secret@") {
		t.Fatalf("unexpected assembled URL: %s", got)
	}
}
