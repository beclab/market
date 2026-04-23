// Package payload contains the typed Go structs that back JSONB columns on
// the database models for which no business-domain struct exists yet (or
// for which keeping the on-disk schema decoupled from the API type is
// intentional).
//
// JSONB columns whose payload is already modelled in market/internal/v2/types
// reference those types directly from the model files via JSONB[types.XXX];
// they do NOT live here.
//
// The structs below are intentionally left as empty starting points. Add
// fields as the corresponding business logic lands; reads/writes are
// transparently handled by db/models.JSONB[T].
package payload

// UserAppStateSpec backs user_application_states.spec.
type UserAppStateSpec struct{}

// UserAppStateStatus backs user_application_states.status.
type UserAppStateStatus struct{}

// MarketSourceOthers backs market_sources.others.
type MarketSourceOthers struct{}
