package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"market/internal/v2/utils"

	"github.com/golang/glog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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
	LogLevel      logger.LogLevel
}

// LoadConfigFromEnv reads PostgreSQL configuration from environment variables,
// falling back to sensible defaults for local development.
//
// The defaults match the legacy task / history modules so that all callers
// hit the same database when POSTGRES_DB is left unset.
func LoadConfigFromEnv() Config {
	return Config{
		Host:     utils.GetEnvOrDefault("POSTGRES_HOST", "localhost"),
		Port:     utils.GetEnvOrDefault("POSTGRES_PORT", "5432"),
		User:     utils.GetEnvOrDefault("POSTGRES_USER", "postgres"),
		Password: utils.GetEnvOrDefault("POSTGRES_PASSWORD", "password"),
		DBName:   utils.GetEnvOrDefault("POSTGRES_DB", "history"),
		SSLMode:  utils.GetEnvOrDefault("POSTGRES_SSLMODE", "disable"),
		TimeZone: utils.GetEnvOrDefault("POSTGRES_TIMEZONE", "UTC"),

		MaxOpenConns:    getEnvInt("POSTGRES_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvInt("POSTGRES_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: getEnvDuration("POSTGRES_CONN_MAX_LIFETIME", 30*time.Minute),
		ConnectTimeout:  getEnvDuration("POSTGRES_CONNECT_TIMEOUT", 10*time.Second),

		SlowThreshold: getEnvDuration("POSTGRES_SLOW_THRESHOLD", 200*time.Millisecond),
		LogLevel:      parseLogLevel(utils.GetEnvOrDefault("POSTGRES_LOG_LEVEL", "warn")),
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
		Logger: logger.New(&glogWriter{}, logger.Config{
			SlowThreshold:             cfg.SlowThreshold,
			LogLevel:                  cfg.LogLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
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

// glogWriter adapts gorm logger output to glog.
type glogWriter struct{}

func (glogWriter) Printf(format string, args ...interface{}) {
	glog.InfoDepth(1, fmt.Sprintf(format, args...))
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseLogLevel(s string) logger.LogLevel {
	switch s {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "warn":
		return logger.Warn
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}
