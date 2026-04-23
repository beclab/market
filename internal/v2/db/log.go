package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// glogGormLogger implements gorm/logger.Interface and routes every gorm log
// callback to the appropriate glog severity:
//
//   - Info  -> glog.V(3).Infof(...)
//   - Warn  -> glog.Warningf(...)
//   - Error -> glog.Errorf(...)
//   - Trace
//       failed query           -> glog.Errorf("[gorm] ...")
//       slow query             -> glog.Warningf("[gorm-slow] ...")
//       successful query       -> glog.V(4).Infof("[gorm] ...")
//
// This keeps gorm's diagnostics in glog's level / -v based filtering pipeline
// instead of forcing everything through glog.Info as a plain Printf adapter
// would.
type glogGormLogger struct {
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

// newGlogGormLogger constructs a logger with a fixed level and slow-query
// threshold. The level only gates Info(); Warn and Error always reach glog.
func newGlogGormLogger(level gormlogger.LogLevel, slowThreshold time.Duration) *glogGormLogger {
	return &glogGormLogger{level: level, slowThreshold: slowThreshold}
}

// LogMode returns a copy of the logger with the requested level. Required by
// the gorm logger.Interface; gorm calls this when AutoMigrate / Session /
// other operations need to scope the level temporarily.
func (l *glogGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *glogGormLogger) Info(_ context.Context, msg string, args ...interface{}) {
	if l.level < gormlogger.Info {
		return
	}
	if !bool(glog.V(3)) {
		return
	}
	glog.InfoDepthf(2, "[gorm] "+msg, args...)
}

func (l *glogGormLogger) Warn(_ context.Context, msg string, args ...interface{}) {
	if l.level < gormlogger.Warn {
		return
	}
	glog.WarningDepthf(2, "[gorm] "+msg, args...)
}

func (l *glogGormLogger) Error(_ context.Context, msg string, args ...interface{}) {
	if l.level < gormlogger.Error {
		return
	}
	glog.ErrorDepthf(2, "[gorm] "+msg, args...)
}

// Trace is invoked by gorm for every executed SQL statement. We avoid calling
// fc() (which materialises the SQL string + args) until we know the row will
// actually be emitted, to keep the no-log path cheap.
func (l *glogGormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error:
		sql, rows := fc()
		glog.ErrorDepthf(2, "[gorm] err=%v duration=%s rows=%s sql=%q",
			err, elapsed, formatRows(rows), sql)
	case l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		glog.WarningDepthf(2, "[gorm-slow] duration=%s threshold=%s rows=%s sql=%q",
			elapsed, l.slowThreshold, formatRows(rows), sql)
	case l.level >= gormlogger.Info && bool(glog.V(4)):
		sql, rows := fc()
		glog.InfoDepthf(2, "[gorm] duration=%s rows=%s sql=%q",
			elapsed, formatRows(rows), sql)
	}
}

// formatRows mirrors gorm's convention of printing "-" when no row count is
// available (for example, on errors before execution).
func formatRows(n int64) string {
	if n < 0 {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}
