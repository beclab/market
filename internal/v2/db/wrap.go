package db

import "github.com/jmoiron/sqlx"

// GlobalSqlxDB returns a *sqlx.DB backed by the same shared connection pool
// exposed through Global(). Both views ultimately wrap the same *sql.DB so
// connections are not duplicated and the pool sizing configured in Open()
// applies to all callers regardless of whether they use gorm or sqlx.
//
// db.Open + SetGlobal MUST run before this call; otherwise it returns nil.
// Callers MUST NOT close the returned handle: closing it would tear down
// the shared *sql.DB used by every other consumer in the process.
//
// Wrapping is intentionally not memoised. sqlx.NewDb is a struct-literal
// constructor with no IO and no extra connections, so per-call allocation
// is negligible and avoiding cached package state keeps the helper
// trivially testable.
func GlobalSqlxDB() *sqlx.DB {
	if globalDB == nil {
		return nil
	}
	sqlDB, err := globalDB.DB()
	if err != nil {
		return nil
	}
	return sqlx.NewDb(sqlDB, "postgres")
}
