// Package whatsappadapter is the ONLY package that imports go.mau.fi/whatsmeow.
// All WhatsApp protocol interaction is isolated here (hexagonal boundary).
// Full implementation will be added in Phase 1 Plans 03-04.
package whatsappadapter

import (
	"database/sql"

	"go.mau.fi/whatsmeow"
	"modernc.org/sqlite"
)

// Adapter wraps a whatsmeow client. Stub for Phase 1 Plan 01.
type Adapter struct {
	client *whatsmeow.Client
}

func init() {
	// Register alias so sqlstore can use dialect "sqlite3".
	// modernc.org/sqlite registers itself as "sqlite"; whatsmeow sqlstore requires "sqlite3".
	sql.Register("sqlite3", &sqlite.Driver{})
}
