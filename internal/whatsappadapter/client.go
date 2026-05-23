// Package whatsappadapter is the ONLY package that imports go.mau.fi/whatsmeow.
// All WhatsApp protocol interaction is isolated here (hexagonal boundary).
package whatsappadapter

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mdp/qrterminal/v3"
	whatsmeow "go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"modernc.org/sqlite"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/pipeline"
)

func init() {
	// Register the "sqlite3" alias required by sqlstore's dialect parameter.
	// modernc.org/sqlite registers itself as "sqlite"; sqlstore.NewWithDB requires "sqlite3".
	// sql.Open still uses "sqlite" (the original driver name); "sqlite3" is only for the dialect.
	sql.Register("sqlite3", &sqlite.Driver{})
}

// Adapter wraps a whatsmeow client and provides the WhatsApp connection lifecycle.
// It is the only type in the project that is allowed to import whatsmeow.
type Adapter struct {
	client    *whatsmeow.Client
	db        *sql.DB
	cfg       *config.Store
	pipeline  *pipeline.Pipeline
	cancel    context.CancelFunc // stored to signal shutdown from event handler (never call Disconnect from handler)
	startTime time.Time          // recorded in New() before any Connect; used for HistorySync flood filter (D-07)
	botJID    string             // bot's own JID in non-AD form; set after Connect() for quote-chain prevention
	botLID    string             // bot's LID (@lid) used in mention MentionedJID on newer WhatsApp clients
}

// New returns an uninitialised Adapter. startTime is recorded here — before any Connect
// call — so that the HistorySync flood filter (D-07) can drop all replayed messages
// predating bot startup.
func New(cfg *config.Store, pipe *pipeline.Pipeline) *Adapter {
	return &Adapter{
		cfg:       cfg,
		pipeline:  pipe,
		startTime: time.Now(),
	}
}

// Start opens the SQLite session store, runs a PRAGMA integrity_check, initialises the
// whatsmeow sqlstore container, obtains (or creates) the first device, registers the
// event handler, starts the QR pairing flow on first launch, and connects to WhatsApp.
//
// The context passed in should be the app-level context from cmd/bot/main.go.  Start
// wraps it with context.WithCancel so that lifecycle events (LoggedOut, StreamReplaced)
// can signal shutdown via a.cancel() without calling Disconnect from inside the event
// handler goroutine (which would deadlock — see RESEARCH.md Pitfall 6).
func (a *Adapter) Start(ctx context.Context) error {
	// Derive a cancellable child context so the event handler can cancel it without
	// calling Disconnect() (deadlock prevention).
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	snap := a.cfg.Get()
	dbPath := snap.DB.Path

	// Step 1: Open SQLite database using the original driver name ("sqlite").
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		cancel()
		return fmt.Errorf("open sqlite: %w", err)
	}
	a.db = db

	// Step 2: PRAGMA integrity_check — must run before handing db to sqlstore (SESSION-03 / T-03-04).
	var integrityResult string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrityResult); err != nil {
		db.Close()
		cancel()
		return fmt.Errorf("SQLite integrity check failed: result=%q err=%v", integrityResult, err)
	}
	if integrityResult != "ok" {
		db.Close()
		cancel()
		return fmt.Errorf("SQLite integrity check failed: result=%q err=%v", integrityResult, nil)
	}

	// Step 3: Create sqlstore container. The dialect string is "sqlite3" (the alias we
	// registered in init()) — a separate concept from the database/sql driver name.
	container := sqlstore.NewWithDB(db, "sqlite3", newWALogger("sqlstore"))

	// Step 4: Upgrade schema. NewWithDB does NOT auto-upgrade — this must be explicit.
	if err := container.Upgrade(ctx); err != nil {
		db.Close()
		cancel()
		return fmt.Errorf("sqlstore upgrade: %w", err)
	}

	// Step 5: Obtain the first (or only) device. Creates a new device entry if none exists.
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		db.Close()
		cancel()
		return fmt.Errorf("get first device: %w", err)
	}

	// Step 6: Create the whatsmeow client.
	a.client = whatsmeow.NewClient(device, newWALogger("whatsmeow"))

	// Step 7: Register event handler before Connect so no events are missed.
	a.client.AddEventHandler(a.onEvent)

	// Step 8: QR pairing or resume.
	// If client.Store.ID == nil, no session has been persisted yet — initiate QR flow.
	if a.client.Store.ID == nil {
		qrChan, err := a.client.GetQRChannel(ctx)
		if err != nil {
			db.Close()
			cancel()
			return fmt.Errorf("get QR channel: %w", err)
		}
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					qrterminal.GenerateWithConfig(evt.Code, qrterminal.Config{
						Level:     qrterminal.L,
						Writer:    os.Stdout,
						BlackChar: qrterminal.BLACK,
						WhiteChar: qrterminal.WHITE,
						QuietZone: 1,
					})
					fmt.Println("Or paste into a QR generator:", evt.Code)
				}
			}
		}()
	}

	// Step 9: Connect. Returns when the connection is established (or fails).
	if err := a.client.Connect(); err != nil {
		db.Close()
		cancel()
		return fmt.Errorf("whatsmeow connect: %w", err)
	}

	// Step 10: Record the bot's own JID and LID for mention matching and quote-chain prevention.
	// Newer WhatsApp clients send MentionedJID entries in @lid form; we must check both.
	if a.client.Store.ID != nil {
		a.botJID = a.client.Store.ID.ToNonAD().String()
	}
	if !a.client.Store.LID.IsEmpty() {
		a.botLID = a.client.Store.LID.ToNonAD().String()
	}
	slog.Info("bot identity recorded",
		"bot_jid", a.botJID,
		"bot_lid", a.botLID,
	)

	// Step 11: Log all groups the device is currently part of.
	a.logJoinedGroups()

	return nil
}

func (a *Adapter) logJoinedGroups() {
	groups, err := a.client.GetJoinedGroups(context.Background())
	if err != nil {
		slog.Warn("could not fetch joined groups", "err", err)
		return
	}
	for _, g := range groups {
		slog.Info("joined group", "jid", g.JID.String(), "name", g.Name)
	}
}

// Disconnect cleanly shuts down the WhatsApp client and closes the SQLite database.
// This must ONLY be called from outside the event handler goroutine (e.g., from app.go
// after ctx.Done() fires) — calling it from inside the event handler deadlocks.
func (a *Adapter) Disconnect() {
	if a.client != nil {
		a.client.Disconnect()
	}
	if a.db != nil {
		a.db.Close()
	}
}
