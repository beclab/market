package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSONB is a generic wrapper that maps a Go struct to a PostgreSQL jsonb
// column. Embed it on a model field with `gorm:"type:jsonb"` and access the
// payload through the .Data field.
//
// Example:
//
//	type Application struct {
//	    AppEntry JSONB[payload.AppEntry] `gorm:"type:jsonb"`
//	}
//
//	app.AppEntry.Data.Icon = "..."
type JSONB[T any] struct {
	Data T
}

// NewJSONB is a small constructor helper.
func NewJSONB[T any](v T) JSONB[T] { return JSONB[T]{Data: v} }

// Value implements the driver.Valuer interface so GORM can serialise the
// payload into jsonb.
func (j JSONB[T]) Value() (driver.Value, error) {
	return json.Marshal(j.Data)
}

// Scan implements sql.Scanner so GORM can deserialise jsonb into the payload.
func (j *JSONB[T]) Scan(src any) error {
	if src == nil {
		var zero T
		j.Data = zero
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("JSONB: unsupported scan type %T", src)
	}
	if len(b) == 0 {
		var zero T
		j.Data = zero
		return nil
	}
	return json.Unmarshal(b, &j.Data)
}

// GormDataType reports the abstract type to GORM.
func (JSONB[T]) GormDataType() string { return "jsonb" }

// GormDBDataType reports the concrete column type for the active dialect.
// Only PostgreSQL is supported; other dialects fall back to "json".
func (JSONB[T]) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db == nil {
		return "jsonb"
	}
	switch db.Dialector.Name() {
	case "postgres":
		return "jsonb"
	default:
		return "json"
	}
}

// MarshalJSON makes JSONB transparent when the surrounding struct is itself
// being marshalled (e.g. for API responses).
func (j JSONB[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(j.Data)
}

// UnmarshalJSON mirrors MarshalJSON for symmetry when decoding API payloads.
func (j *JSONB[T]) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &j.Data)
}
