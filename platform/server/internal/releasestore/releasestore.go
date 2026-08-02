package releasestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusReleasing    = "releasing"
	StatusStable       = "stable"
	StatusFailed       = "failed"
	StatusCompensating = "compensating"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ReleaseRecord struct {
	Service         string     `json:"service"`
	Environment     string     `json:"environment"`
	GitRevision     string     `json:"git_revision"`
	ImageDigest     string     `json:"image_digest"`
	ConfigRevision  string     `json:"config_revision"`
	RolloutStrategy string     `json:"rollout_strategy"`
	ReleaseStatus   string     `json:"release_status"`
	ReleasedAt      *time.Time `json:"released_at"`
}

func (r ReleaseRecord) Validate() error {
	if strings.TrimSpace(r.Service) == "" {
		return errors.New("service is required")
	}
	if strings.TrimSpace(r.Environment) == "" {
		return errors.New("environment is required")
	}
	switch r.ReleaseStatus {
	case StatusReleasing, StatusStable, StatusFailed, StatusCompensating:
	default:
		return fmt.Errorf("unsupported release status %q", r.ReleaseStatus)
	}
	if r.ImageDigest != "" && !digestPattern.MatchString(r.ImageDigest) {
		return fmt.Errorf("image_digest must be sha256:<64 hex>")
	}
	return nil
}

func (r ReleaseRecord) WithDefaults() ReleaseRecord {
	if r.Environment == "" {
		r.Environment = "dev"
	}
	return r
}

// Record writes one release state transition. A stable status upserts the
// single stable row per service/environment; every other status appends a
// historical transition row.
func Record(ctx context.Context, connString string, record ReleaseRecord) error {
	record = record.WithDefaults()
	if err := record.Validate(); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return fmt.Errorf("connect to release store: %w", err)
	}
	defer pool.Close()

	if record.ReleaseStatus == StatusStable {
		_, err = pool.Exec(ctx, `
			insert into service_releases
			  (service, environment, git_revision, image_digest, config_revision,
			   rollout_strategy, release_status, released_at)
			values ($1, $2, $3, $4, $5, $6, 'stable', now())
			on conflict (service, environment) where release_status = 'stable'
			do update set
			  git_revision = excluded.git_revision,
			  image_digest = excluded.image_digest,
			  config_revision = excluded.config_revision,
			  rollout_strategy = excluded.rollout_strategy,
			  released_at = now(),
			  updated_at = now()`,
			record.Service, record.Environment, record.GitRevision,
			record.ImageDigest, record.ConfigRevision, record.RolloutStrategy)
	} else {
		_, err = pool.Exec(ctx, `
			insert into service_releases
			  (service, environment, git_revision, image_digest, config_revision,
			   rollout_strategy, release_status)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			record.Service, record.Environment, record.GitRevision,
			record.ImageDigest, record.ConfigRevision, record.RolloutStrategy,
			record.ReleaseStatus)
	}
	if err != nil {
		return fmt.Errorf("record release: %w", err)
	}
	return nil
}

// StableDigest returns the most recently verified stable version for a
// service and environment.
func StableDigest(ctx context.Context, connString, service, environment string) (ReleaseRecord, error) {
	if strings.TrimSpace(service) == "" {
		return ReleaseRecord{}, errors.New("service is required")
	}
	if environment == "" {
		environment = "dev"
	}
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return ReleaseRecord{}, fmt.Errorf("connect to release store: %w", err)
	}
	defer pool.Close()

	var record ReleaseRecord
	err = pool.QueryRow(ctx, `
		select service, environment, git_revision, image_digest, config_revision,
		       rollout_strategy, released_at
		from service_releases
		where release_status = 'stable' and service = $1 and environment = $2
		order by released_at desc nulls last, updated_at desc
		limit 1`,
		service, environment,
	).Scan(
		&record.Service, &record.Environment, &record.GitRevision,
		&record.ImageDigest, &record.ConfigRevision, &record.RolloutStrategy,
		&record.ReleasedAt,
	)
	if err != nil {
		return ReleaseRecord{}, fmt.Errorf("query stable digest: %w", err)
	}
	record.ReleaseStatus = StatusStable
	return record, nil
}

// ResolveConnString prefers DATABASE_URL and otherwise assembles a libpq
// style URL from the standard PG* environment variables.
func ResolveConnString(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}
	host := getEnv("PGHOST", "localhost")
	port := getEnv("PGPORT", "5432")
	user := getEnv("PGUSER", "postgres")
	password := os.Getenv("PGPASSWORD")
	database := getEnv("PGDATABASE", "postgres")
	auth := user
	if password != "" {
		auth = user + ":" + password
	}
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", auth, host, port, database)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
