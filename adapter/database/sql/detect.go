package sql

import (
	stdsql "database/sql"
	"fmt"
	"reflect"
	"strings"
)

// resolveDialect best-effort auto-detects a built-in Dialect from db's driver
// type, for the common zero-config case where the caller did not pass
// WithDialect (ADR 0010 D3). It reflects on the value returned by the stdlib
// driver.Driver accessor — it imports NO SQL driver, so the "core imports no
// driver" rule (ADR 0003) holds.
//
// Detection is a substring match on the driver's package path (or, as a
// fallback, its type name): "pq"/"pgx"/"postgres" -> PostgresDialect(). The
// MySQL branch is added in a later increment. A non-matching driver returns
// ErrDialectUndetected naming the driver type, telling the caller to pass
// WithDialect. Auto-detect is heuristic (a Postgres-wire derivative with
// different SKIP LOCKED/RETURNING semantics is mis-detected as vanilla
// Postgres); WithDialect is the only guaranteed-correct path.
func resolveDialect(db *stdsql.DB) (Dialect, error) {
	drv := db.Driver()
	rt := reflect.TypeOf(drv)

	// The driver is typically a pointer (e.g. *stdlib.Driver for pgx), and a
	// pointer type has an empty PkgPath, so dereference to the named element
	// type whose package path carries the "pgx"/"pq" marker.
	et := rt
	for et != nil && et.Kind() == reflect.Ptr {
		et = et.Elem()
	}

	// Haystack: the element type's package path (primary signal) plus the raw
	// type name (fallback for an oddly-shaped driver whose PkgPath is empty).
	var haystack string
	if et != nil {
		haystack = et.PkgPath()
	}
	if haystack == "" && rt != nil {
		haystack = rt.String()
	}
	lower := strings.ToLower(haystack)

	switch {
	case strings.Contains(lower, "pgx"),
		strings.Contains(lower, "postgres"),
		strings.Contains(lower, "pq"):
		return PostgresDialect(), nil
	default:
		return nil, fmt.Errorf("%w: driver type %s; pass WithDialect to select one explicitly",
			ErrDialectUndetected, driverTypeName(rt))
	}
}

// driverTypeName renders a driver type for the ErrDialectUndetected message,
// tolerating a nil type (a driver that reflects to nil).
func driverTypeName(rt reflect.Type) string {
	if rt == nil {
		return "<nil>"
	}
	return rt.String()
}
