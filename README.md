# bot-camomila

WhatsApp de-escalation bot for a single group chat. Fuzzy-matches keywords (Levenshtein distance) or @mentions in messages and replies with a randomly-picked calming answer threaded to the triggering message.

## Requirements

- Go 1.26.3+
- A WhatsApp account to pair with (phone with WA installed)

## Quick start

```sh
# Clone and enter the repo
git clone https://github.com/taldoflemis/bot-camomila
cd bot-camomila

# Copy the example config and edit it
cp config.yaml my-config.yaml
$EDITOR my-config.yaml

# Run
go run ./cmd/bot --config my-config.yaml
```

On first run, a QR code is printed to stdout. Scan it with WhatsApp on your phone (Linked Devices → Link a Device). The session is persisted to the SQLite file configured in `db.path` — subsequent runs resume without re-scanning.

## Config

```yaml
# yaml-language-server: $schema=./config.schema.json

log:
  level: info

listeners:
  - group_jid: "asdfasdfasdfasdf@g.us"
    allow_admin_commands: true
    matchers:
      - mention
      - little

  - group_jid: "qwerqwerqwerqwer@g.us"
    matchers:
      - mention

matchers:
  - name: little
    levenshtein:
      words: ["fouda", "hello", "wilson"]
      distance: 1
      cluster: little
      cooldown_sec: 5
  - name: mention
    mention:
      cluster: mention
      cooldown_sec: 5

limits:
  user_cooldown_sec: 5

clusters:
  - name: little
    answers:
      - "testing {REPLIED_USER}"
      - "testing {MATCHED_WORD} asdfasdf sefaz"
      - "testing gabriel {REPLIED_USER}"

  - name: mention
    answers:
      - "quem me marcar é viado"

db:
  path: "./session.sqlite"
```

### Finding your group JID

Run the bot once — at startup it logs all joined groups:

```
level=INFO msg="joined group" jid=120363428452727309@g.us name="My Group"
```

Copy the `jid` value into `listeners[].group_jid`.

### Matcher kinds

| Kind          | Fires when                                       | Required fields                |
| ------------- | ------------------------------------------------ | ------------------------------ |
| `levenshtein` | message token is within `distance` of any `word` | `words`, `distance`, `cluster` |
| `mention`     | bot is @mentioned in the message                 | `cluster`                      |

**Levenshtein distance rules:**

- `distance: 0` — exact match only
- `distance: 1` — words must be ≥ 5 runes
- `distance: 2` — words must be ≥ 8 runes

### Answer placeholders

| Placeholder      | Replaced with                      |
| ---------------- | ---------------------------------- |
| `{REPLIED_USER}` | sender's push name                 |
| `{MATCHED_WORD}` | the token that triggered the match |

### Multiple groups

Add more entries to `listeners`. Each group gets its own ordered matcher list. Global `matchers` and `clusters` are shared across all listeners.

```yaml
listeners:
  - group_jid: "111111111@g.us"
    matchers: [mention_reply, sefaz_pequeno]
  - group_jid: "222222222@g.us"
    matchers: [mention_reply]
```

## Running from a Pre-built Binary

You can download a pre-built binary for your architecture from the [GitHub Releases](https://github.com/taldoflemis/bot-camomila/releases) page.

```sh
# Example for Linux AMD64
wget https://github.com/taldoflemis/bot-camomila/releases/latest/download/bot-camomila-linux-amd64
chmod +x bot-camomila-linux-amd64
./bot-camomila-linux-amd64 --config config.yaml
```

## Building a binary from source

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bot ./cmd/bot
./bot --config config.yaml
```

## Running with Docker

```sh
docker build -t bot-camomila .

docker run -d \
  -v /path/to/config.yaml:/config.yaml:ro \
  -v /path/to/data:/data \
  -e BOT_CONFIG=/config.yaml \
  bot-camomila
```

Set `db.path: "/data/session.sqlite"` so the session survives container restarts.

## Config hot-reload

Edit `config.yaml` while the bot is running — changes take effect within ~500 ms without restart. The kill switch state (`!pause` / `!resume`) is preserved across reloads.

## Owner commands

Commands can be sent directly from the bot's own WhatsApp account (via a paired companion device or the main phone), or by group admins if `allow_admin_commands: true` is configured for the group.

| Command   | Effect                    |
| --------- | ------------------------- |
| `!pause`  | Silences the bot globally |
| `!resume` | Re-enables the bot        |

## Running tests

```sh
go test ./...
```

