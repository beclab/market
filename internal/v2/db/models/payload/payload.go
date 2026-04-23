// Package payload contains the typed Go structs that back JSONB columns on
// the database models. They are kept in a dedicated package so they can be
// reused (e.g. by API handlers) without pulling in the GORM models.
//
// The structs below are intentionally left as empty starting points. Add
// fields as the corresponding business logic lands; reads/writes are
// transparently handled by db/models.JSONB[T].
package payload

// AppEntry backs application.app_entry.
type AppEntry struct{}

// AppImageAnalysis backs application.app_image_analysis and
// user_application.app_image_analysis.
type AppImageAnalysis struct{}

// UserAppRawData backs user_application.app_raw_data.
type UserAppRawData struct{}

// UserAppStateSpec backs user_application_state.spec.
type UserAppStateSpec struct{}

// UserAppStateStatus backs user_application_state.status.
type UserAppStateStatus struct{}

// MarketSourceOthers backs market_source.others.
type MarketSourceOthers struct{}
