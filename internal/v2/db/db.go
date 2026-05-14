package db

import (
	"context"
	"fmt"
	"time"

	"market/internal/v2/helper"

	"github.com/golang/glog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// defaultGormLogLevel is the gorm logger threshold baked into the binary.
// Errors and warnings (including slow queries) are always emitted; Info and
// per-query Trace lines are gated by glog -v levels inside glogGormLogger.
const defaultGormLogLevel = gormlogger.Warn

// Config holds PostgreSQL connection and pool settings.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	TimeZone string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnectTimeout  time.Duration

	SlowThreshold time.Duration
}

// LoadConfigFromEnv reads PostgreSQL configuration from the environment.
//
// Only the five connection-target fields are sourced from env (POSTGRES_HOST,
// POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB). The
// remaining fields are deliberately hardcoded to values vetted against our
// PostgreSQL instance (max_connections=1000, modest server footprint):
//
//   - SSLMode "disable":        PG runs inside the cluster network.
//   - TimeZone "UTC":           UTC in the database, conversion at the edge.
//   - MaxOpenConns 50:          ~5% of the server's max_connections, gives
//                               2-3x headroom over expected concurrency.
//   - MaxIdleConns 10:          1/5 of MaxOpen, the conventional ratio.
//   - ConnMaxLifetime 30m:      well below the server's 2h tcp_keepalives_idle,
//                               so connections rotate before they go stale and
//                               the pool recovers cleanly from PG restarts.
//   - ConnectTimeout 10s:       intra-cluster connects take <1s, this is just
//                               a startup-jitter buffer.
//   - SlowThreshold 200ms:      the gorm standard; emitted with [gorm-slow].
//
// The defaults for the env-backed fields match the legacy task / history
// modules so that all callers hit the same database when POSTGRES_DB is left
// unset.
func LoadConfigFromEnv() Config {
	return Config{
		Host:     helper.GetEnvOrDefault("POSTGRES_HOST", "localhost"),
		Port:     helper.GetEnvOrDefault("POSTGRES_PORT", "5432"),
		User:     helper.GetEnvOrDefault("POSTGRES_USER", "postgres"),
		Password: helper.GetEnvOrDefault("POSTGRES_PASSWORD", "password"),
		DBName:   helper.GetEnvOrDefault("POSTGRES_DB", "history"),

		SSLMode:  "disable",
		TimeZone: "UTC",

		MaxOpenConns:    50,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		ConnectTimeout:  10 * time.Second,

		SlowThreshold: 200 * time.Millisecond,
	}
}

// DSN renders the libpq-style connection string.
func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s connect_timeout=%d",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode, c.TimeZone,
		int(c.ConnectTimeout.Seconds()),
	)
}

// Open establishes a *gorm.DB, configures the underlying connection pool and
// verifies connectivity with a Ping. It does NOT run migrations.
//
// All models in db/models declare their TableName() explicitly, so we leave
// the default naming strategy in place and rely on the per-model overrides.
func Open(ctx context.Context, cfg Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger:  newGlogGormLogger(defaultGormLogLevel, cfg.SlowThreshold),
		NowFunc: func() time.Time { return time.Now().UTC() },
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  cfg.DSN(),
		PreferSimpleProtocol: false,
	}), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire underlying *sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	glog.V(2).Infof("PostgreSQL connected: host=%s port=%s db=%s", cfg.Host, cfg.Port, cfg.DBName)
	return db, nil
}

// Close releases the underlying *sql.DB.
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Ping checks connectivity using the supplied context.
func Ping(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// globalDB holds the package-level singleton. It is written exactly once at
// startup (from main.go via SetGlobal) and read concurrently by the rest of
// the application after that. No synchronisation is required because the
// write happens-before any goroutine that subsequently reads it: SetGlobal
// is invoked synchronously in main before history / task / appinfo modules
// are constructed.
var globalDB *gorm.DB

// SetGlobal stores the *gorm.DB as the package-level singleton.
// Intended as a transitional convenience until callers move to dependency
// injection.
func SetGlobal(db *gorm.DB) { globalDB = db }

// Global returns the singleton *gorm.DB previously installed via SetGlobal.
func Global() *gorm.DB { return globalDB }
