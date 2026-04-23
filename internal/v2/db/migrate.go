package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// migrationsDir is the directory inside embeddedMigrations that goose reads.
const migrationsDir = "migrations"

// advisoryLockKey is a fixed 64-bit key used with pg_advisory_lock to
// serialize migrations across multiple replicas. Derived from the ASCII
// bytes of "marketdb".
const advisoryLockKey int64 = 0x6D61726B6574_6462

// MigrateDirection enumerates the supported migration operations.
type MigrateDirection int

const (
	// MigrateUp applies all pending migrations.
	MigrateUp MigrateDirection = iota
	// MigrateStatusOnly only reports current status without applying anything.
	MigrateStatusOnly
)

// MigrateOptions controls Migrate behaviour.
type MigrateOptions struct {
	Direction   MigrateDirection
	LockTimeout time.Duration
	DryRun      bool
}

// MigrationRecord describes a single migration's state.
type MigrationRecord struct {
	Version int64
	Name    string
	Pending bool
}

// Migrate runs pending migrations against db using the embedded SQL files. It
// guards against concurrent execution from multiple replicas using a
// PostgreSQL session-level advisory lock.
func Migrate(ctx context.Context, db *gorm.DB, opts MigrateOptions) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("acquire underlying *sql.DB: %w", err)
	}

	if err := configureGoose(); err != nil {
		return err
	}

	lockTimeout := opts.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = 30 * time.Second
	}

	conn, err := acquireAdvisoryLock(ctx, sqlDB, lockTimeout)
	if err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer releaseAdvisoryLock(conn)

	if opts.Direction == MigrateStatusOnly || opts.DryRun {
		glog.V(2).Info("Migration dry-run / status check")
		if err := goose.StatusContext(ctx, sqlDB, migrationsDir); err != nil {
			return fmt.Errorf("goose status: %w", err)
		}
		if opts.DryRun || opts.Direction == MigrateStatusOnly {
			return nil
		}
	}

	switch opts.Direction {
	case MigrateUp:
		glog.V(2).Info("Running pending DB migrations (up)")
		if err := goose.UpContext(ctx, sqlDB, migrationsDir); err != nil {
			return fmt.Errorf("goose up: %w", err)
		}
		ver, err := goose.GetDBVersionContext(ctx, sqlDB)
		if err == nil {
			glog.V(2).Infof("DB migration completed; current version = %d", ver)
		}
		return nil
	default:
		return fmt.Errorf("unsupported migrate direction: %d", opts.Direction)
	}
}

// MigrationStatus returns the applied/pending status of every known migration.
func MigrationStatus(ctx context.Context, db *gorm.DB) ([]MigrationRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if err := configureGoose(); err != nil {
		return nil, err
	}

	migrations, err := goose.CollectMigrations(migrationsDir, 0, goose.MaxVersion)
	if err != nil {
		return nil, fmt.Errorf("collect migrations: %w", err)
	}

	currentVersion, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return nil, fmt.Errorf("get db version: %w", err)
	}

	out := make([]MigrationRecord, 0, len(migrations))
	for _, m := range migrations {
		out = append(out, MigrationRecord{
			Version: m.Version,
			Name:    m.Source,
			Pending: m.Version > currentVersion,
		})
	}
	return out, nil
}

// configureGoose installs the embedded FS, dialect, table name and logger.
// Goose's API is package-global so this is idempotent.
func configureGoose() error {
	goose.SetBaseFS(embeddedMigrations)
	goose.SetTableName("schema_migrations")
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	goose.SetLogger(gooseLogger{})
	return nil
}

// acquireAdvisoryLock pins a single connection from the pool and obtains a
// session-level PG advisory lock on it. The returned *sql.Conn must be passed
// to releaseAdvisoryLock so the lock is dropped and the connection returned to
// the pool. It polls pg_try_advisory_lock until success or timeout so it
// respects ctx cancellation.
func acquireAdvisoryLock(ctx context.Context, db *sql.DB, timeout time.Duration) (*sql.Conn, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := db.Conn(deadlineCtx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}

	const pollInterval = 500 * time.Millisecond
	for {
		var got bool
		if err := conn.QueryRowContext(deadlineCtx,
			"SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&got); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("pg_try_advisory_lock: %w", err)
		}
		if got {
			return conn, nil
		}
		glog.V(3).Info("Migration lock held by another process; retrying...")
		select {
		case <-deadlineCtx.Done():
			_ = conn.Close()
			return nil, fmt.Errorf("timeout waiting for migration lock: %w", deadlineCtx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// releaseAdvisoryLock releases the session-level lock and returns the pinned
// connection to the pool. Errors are logged but not returned because release
// runs in a defer chain after the primary work has succeeded or failed.
func releaseAdvisoryLock(conn *sql.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx,
		"SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
		glog.Warningf("pg_advisory_unlock failed: %v", err)
	}
	if err := conn.Close(); err != nil {
		glog.Warningf("release migration connection: %v", err)
	}
}

// gooseLogger adapts goose log output to glog so migration progress shows up
// alongside the rest of the application logs.
//
// Goose's logger.Logger interface only exposes Fatalf and Printf. We map
// Fatalf to glog.Errorf rather than glog.Fatalf: any unrecoverable issue is
// already surfaced as an error from goose.UpContext / goose.StatusContext
// and propagated up to main.go, which decides whether to glog.Exitf. Letting
// the logger terminate the process here would bypass the graceful-shutdown
// path in main.
type gooseLogger struct{}

func (gooseLogger) Fatalf(format string, v ...interface{}) {
	glog.Errorf("[goose] "+format, v...)
}
func (gooseLogger) Printf(format string, v ...interface{}) {
	glog.Infof("[goose] "+format, v...)
}
