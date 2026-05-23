---
phase: 2
plan: 6
wave: 3
depends_on: [1, 5]
files_modified:
  - internal/whatsappadapter/inbound.go
  - internal/whatsappadapter/client.go
  - internal/app/app.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "Adapter constructs domain.Message with all Phase 2 fields (QuotedBody, QuotedSenderJID, SenderPushName)"
    - "Adapter calls Pipeline.Handle() and sends reply only when Decision.Reply == true"
    - "Reply is a WhatsApp threaded reply (ExtendedTextMessage + ContextInfo)"
    - "Reply is sent in a goroutine with 2-8s random jitter (REPLY-04)"
    - "Every dispatch decision is logged with structured fields (OBSERV-02)"
    - "Pipeline, cooldown tracker, kill switch, and rate limiter are wired in app.Run()"
  artifacts:
    - "inbound.go wires the pipeline and sends threaded replies"
    - "app.go creates and injects all Phase 2 components"
---

# Plan 2.6: Adapter Integration & Reply Sending

<objective>
Wire the pipeline into the WhatsApp adapter. The adapter constructs a full domain.Message (with quoted text), calls Pipeline.Handle(), logs the decision, and sends a threaded WhatsApp reply with jitter when the decision is to reply.

Purpose: This is where everything comes together — the adapter is the bridge between whatsmeow events and the pure-Go pipeline.
Output: Updated adapter, app.go wiring, and end-to-end reply flow.
</objective>

<context>
Load for context:
- .gsd/SPEC.md (REPLY-01 through REPLY-05, OBSERV-02)
- .gsd/DECISIONS.md (Phase 2 decisions)
- .gsd/research/PITFALLS.md (Pitfall 6: Reply-to-self loops)
- internal/whatsappadapter/inbound.go (current handleMessage with gates)
- internal/whatsappadapter/client.go (Adapter struct)
- internal/app/app.go (composition root)
- internal/pipeline/pipeline.go
- internal/domain/message.go
</context>

<tasks>

<task type="auto">
  <name>Wire pipeline into adapter and send threaded replies</name>
  <files>internal/whatsappadapter/inbound.go, internal/whatsappadapter/client.go</files>
  <action>
    **In client.go:**
    1. Add `pipeline *pipeline.Pipeline` field to the Adapter struct
    2. Add `botJID string` field — set after successful Connect() from `a.client.Store.ID.ToNonAD().String()`
    3. Update `New()` to accept a `*pipeline.Pipeline` parameter:
       `func New(cfg *config.Store, pipe *pipeline.Pipeline) *Adapter`
    4. After `a.client.Connect()` succeeds in Start(), set:
       `a.botJID = a.client.Store.ID.ToNonAD().String()`

    **In inbound.go:**
    1. Update `extractText()` — no changes needed, it already handles conversation and extended text.

    2. Add `extractQuotedText(m *waE2E.Message) (body string, senderJID string)`:
       - Check m.GetExtendedTextMessage().GetContextInfo()
       - If ContextInfo exists and QuotedMessage is not nil:
         - Extract body from QuotedMessage using extractText()
         - Extract sender from ContextInfo.GetParticipant() (string JID)
         - If sender is the bot's own JID (compare with a.botJID): return "", "" (quote-chain prevention)
       - Also check m.GetConversation() — plain text messages don't have ContextInfo
       - Return body, senderJID

    3. Update `handleMessage()`:
       After the existing Gate 3 (text-only filter), REPLACE the Phase 1 terminal log with:

       ```go
       // Construct domain.Message with all Phase 2 fields.
       quotedBody, quotedSender := extractQuotedText(evt.Message)
       msg := domain.Message{
           ID:               evt.Info.ID,
           GroupJID:          evt.Info.Chat.String(),
           SenderJID:        evt.Info.Sender.ToNonAD().String(),
           SenderPushName:   evt.Info.PushName,
           Text:             text,
           QuotedBody:       quotedBody,
           QuotedSenderJID:  quotedSender,
           Timestamp:        evt.Info.Timestamp,
       }

       // Run the pipeline.
       decision := a.pipeline.Handle(msg, snap)

       // Log every dispatch decision (OBSERV-02).
       slog.Info("dispatch decision",
           "event", "dispatch",
           "msg_id", msg.ID,
           "sender_jid", msg.SenderJID,
           "matcher", decision.MatcherName,
           "matched_word", decision.MatchedWord,
           "reply", decision.Reply,
           "drop_reason", decision.DropReason,
       )

       if !decision.Reply {
           return
       }

       // Send reply in a goroutine with jitter (REPLY-04).
       // Do NOT block the event handler — whatsmeow holds a dispatch lock.
       go a.sendReply(evt, decision.Answer)
       ```

    4. Add `sendReply(evt *events.Message, answer string)`:
       ```go
       func (a *Adapter) sendReply(evt *events.Message, answer string) {
           // Random 2-8s jitter (REPLY-04).
           jitter := time.Duration(2+rand.IntN(7)) * time.Second
           time.Sleep(jitter)

           // Build threaded reply (REPLY-01): ExtendedTextMessage with ContextInfo.
           msg := &waE2E.Message{
               ExtendedTextMessage: &waE2E.ExtendedTextMessage{
                   Text: proto.String(answer),
                   ContextInfo: &waE2E.ContextInfo{
                       StanzaId:      proto.String(evt.Info.ID),
                       Participant:   proto.String(evt.Info.Sender.String()),
                       QuotedMessage: evt.Message,
                   },
               },
           }

           ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
           defer cancel()

           _, err := a.client.SendMessage(ctx, evt.Info.Chat, msg)
           if err != nil {
               slog.Error("failed to send reply",
                   "event", "send_error",
                   "msg_id", evt.Info.ID,
                   "err", err,
               )
               return
           }

           slog.Info("reply sent",
               "event", "reply_sent",
               "msg_id", evt.Info.ID,
               "jitter_ms", jitter.Milliseconds(),
           )
       }
       ```

    Add necessary imports:
    - "math/rand/v2" for rand.IntN
    - "context" for timeout
    - "google.golang.org/protobuf/proto" for proto.String
    - pipeline package import

    AVOID: Calling a.client.Disconnect() from sendReply — it runs in a goroutine inside the event handler context.
    AVOID: Using context.Background() without timeout for SendMessage — network stalls must not block forever.
    AVOID: Blocking the event handler with the jitter sleep — the goroutine handles this.
  </action>
  <verify>go build ./... succeeds</verify>
  <done>Adapter constructs domain.Message, calls pipeline, sends threaded reply with jitter in goroutine</done>
</task>

<task type="auto">
  <name>Wire Phase 2 components in app.Run()</name>
  <files>internal/app/app.go</files>
  <action>
    Update app.Run() to create and wire all Phase 2 components:

    After `cfgStore := config.NewStore(snap)` and before creating the adapter:

    ```go
    // Phase 2 — Create pipeline components.
    ks := killswitch.New()
    cd := cooldown.NewTracker(nil) // nil = real clock
    rl := pipeline.NewRateLimiter(nil) // nil = real clock
    pipe := pipeline.New(ks, cd, rl)

    // Start cooldown reaper (cleanup every 5 minutes).
    go cd.StartReaper(ctx, 5*time.Minute)
    ```

    Update the adapter creation:
    ```go
    adapter := whatsappadapter.New(cfgStore, pipe)
    ```

    Add imports for killswitch, cooldown, pipeline packages.

    AVOID: Passing the kill switch to the config store or watcher — it's independent (ADR-003).
    AVOID: Forgetting to start the reaper — without it, cooldown maps grow unbounded.
  </action>
  <verify>go build ./... succeeds</verify>
  <done>app.Run() creates kill switch, cooldown tracker, rate limiter, pipeline, and passes pipeline to adapter</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go build ./...` succeeds with no errors
- [ ] `go vet ./...` passes
- [ ] `go test ./...` — all existing and new tests pass
- [ ] grep confirms: no `time.Local` usage in quiet hours path
- [ ] grep confirms: `sendReply` is called in a goroutine (has `go a.sendReply`)
- [ ] grep confirms: `SendMessage` uses `context.WithTimeout`
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] End-to-end flow: event → gates → match → cooldown → rate cap → jitter → threaded reply
- [ ] All dispatch decisions logged with structured fields
- [ ] Pipeline components wired in app.Run()
</success_criteria>
