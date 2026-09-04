# Proton Mail Tools for OpenWebUI

Give an OpenWebUI assistant a Proton Mail account: search and read mail, send and reply,
mark read/flagged, move, trash, and manage folders and labels — through [Proton Mail Bridge](https://proton.me/mail/bridge)
running headless in a container next to the tools.

- **OpenAPI tool server** — one operation per tool, imported by OpenWebUI in one click.
- **Self-contained** — a pinned, headless Bridge image plus a ~17 MB static Go service;
  no desktop Bridge, no host ports, no firewall rules.
- **Safe by construction** — bearer-key auth, `ALLOW_SEND`/`ALLOW_DELETE` gates, Bridge's
  IMAP/SMTP reachable only over a loopback the two containers share, and a guard against a
  Bridge quirk that would otherwise turn "move to Trash" into a permanent delete.
- **Model-friendly output** — HTML bodies converted to text, bodies truncated to a
  configurable size, newest-first search, stable `mailbox` + `uid` message references.
- **Proton-aware organising** — folders vs. labels are distinguished (`kind` in
  `list_mailboxes`), labels are applied additively, starring maps to `flagged`, and
  archive/trash resolve the right system mailbox by role.

## Tools

| Tool (`operationId`)   | What it does |
|------------------------|--------------|
| `list_mailboxes`       | Folders/labels with role (`inbox`, `sent`, `trash`, …) and unread/total counts |
| `search_messages`      | Filter one mailbox by free text, from, to, subject, date range, unread; newest first |
| `read_message`         | Full message: headers, plain-text body, hyperlinks, `List-Unsubscribe` URLs, attachment list (does **not** mark it read) |
| `send_message`         | New plain-text email (to/cc/bcc) |
| `reply_to_message`     | Threaded reply — recipients, subject and `In-Reply-To`/`References` derived from the original; marks it answered |
| `update_message_flags` | Mark read/unread and flagged/unflagged, one or many uids |
| `move_messages`        | Move to any existing folder |
| `archive_messages`     | Move to Archive |
| `label_messages` / `unlabel_messages` | Add / remove a Proton label (additive tag; the message stays in its folder) |
| `trash_messages`       | Move to Trash (reversible delete for INBOX, folders, labels) |
| `delete_messages` / `empty_mailbox` | Permanent deletion — **Spam and Trash only**, off unless `ALLOW_PURGE=true` |
| `create_mailbox` / `rename_mailbox` / `delete_mailbox` | Create, rename or delete a folder or label; folders must be empty to delete, system mailboxes are refused |

`GET /health` and `GET /openapi.json` are unauthenticated support endpoints and are not
exposed as tools.

## Architecture

```
                 bearer key                          shared network namespace
OpenWebUI ─────────────────▶ proton-mail-tools ──127.0.0.1:1143/1025──▶ proton-bridge ──▶ Proton
        http://proton-mail-tools:8930   (Go, stateless)      STARTTLS         (headless)
```

Two containers, one network namespace:

- **`proton-bridge`** — Proton's official Bridge `.deb`, repacked without desktop
  dependencies and run with `--noninteractive`. Its encrypted vault, `pass`-based
  keychain and mail cache live on the `proton-bridge-data` volume. It owns the network
  namespace and is attached to the Docker network OpenWebUI uses, under the alias
  `proton-mail-tools`.
- **`proton-mail-tools`** — the Go service, joined to Bridge's namespace with
  `network_mode: "service:proton-bridge"`. It talks to Bridge over loopback and listens on
  `:8930`, which is therefore reachable only from that Docker network. Each request opens
  a short-lived IMAP session (login, act, logout), so the service is stateless and safe
  for concurrent tool calls.

Because Bridge owns the namespace, rebuilding or restarting the tool service never
touches Bridge or its sync.

## Requirements

- Docker with Compose v2.
- A Proton Mail account on a plan that includes Bridge (any paid Mail plan).
- OpenWebUI 0.6 or later (native OpenAPI tool servers). Tested with 0.11.
- The OpenWebUI container and `proton-bridge` on a common Docker network.

## Quick start (standalone)

```bash
git clone https://github.com/dividehex/proton-mail-tools.git proton-mail-tools && cd proton-mail-tools
cp .env.example .env
# set PROTON_TOOLS_API_KEY (openssl rand -hex 32) and PROTON_BRIDGE_USERNAME (your Proton address)

docker compose build
docker compose run --rm -it proton-bridge init     # interactive Bridge CLI, see below
docker compose up -d
curl http://127.0.0.1:8930/health
```

In the Bridge CLI:

```
>>> login            # Proton username, password, 2FA
>>> info             # shows the IMAP/SMTP username and the *Bridge password*
>>> exit
```

Copy the Bridge password into `PROTON_BRIDGE_PASSWORD` in `.env` and run
`docker compose up -d` again. Bridge then performs a full mailbox sync; the tools work
during the sync, but older mail appears progressively (hours for large mailboxes).

Never run `init` while the `proton-bridge` service is up — two Bridge processes must not
share a vault.

## Integrating into an existing AI stack

This is the intended deployment: alongside OpenWebUI in the compose project that already
runs your stack. The steps assume a layout like

```
~/ai/
├── compose.yaml            # your stack: openwebui, litellm, ...
├── .env
└── proton-mail-tools/      # this repository
```

1. **Check out the repo** inside the stack directory:

   ```bash
   cd ~/ai && git clone https://github.com/dividehex/proton-mail-tools.git proton-mail-tools
   ```

2. **Add the services.** Paste the two services from
   [`docker-compose.snippet.yml`](docker-compose.snippet.yml) into your `compose.yaml`
   and add `proton-bridge-data:` under its top-level `volumes:`. Change `ai-agent` in the
   snippet to the network your OpenWebUI service is on (`docker inspect <openwebui> --format
   '{{json .NetworkSettings.Networks}}'` shows it). Keep the `proton-mail-tools` network
   alias — that is the hostname OpenWebUI will use.

3. **Add secrets** to the stack's `.env`:

   ```bash
   PROTON_TOOLS_API_KEY="$(openssl rand -hex 32)"
   PROTON_BRIDGE_USERNAME=you@proton.me
   PROTON_BRIDGE_PASSWORD=            # filled in step 4
   ```

   Optional: `PROTON_MAIL_FROM_ADDRESS` / `PROTON_MAIL_FROM_NAME` to send from an alias,
   `PROTON_TOOLS_ALLOW_SEND=false` / `PROTON_TOOLS_ALLOW_DELETE=false` to restrict the agent,
   `PROTON_TOOLS_ALLOW_PURGE=true` to let it empty Spam/Trash permanently.

4. **Log Bridge in** (once):

   ```bash
   docker compose build proton-bridge proton-mail-tools
   docker compose run --rm -it proton-bridge init      # >>> login, >>> info, >>> exit
   ```

   Put the Bridge password printed by `info` into `PROTON_BRIDGE_PASSWORD`.
   Already running Bridge on the desktop with a fully synced mailbox? See
   [docs/migrate-desktop-bridge.md](docs/migrate-desktop-bridge.md) to move that state
   into the container instead of syncing again.

5. **Start and verify** from OpenWebUI's point of view:

   ```bash
   docker compose up -d proton-bridge proton-mail-tools
   docker exec <openwebui-container> curl -s http://proton-mail-tools:8930/health
   docker exec <openwebui-container> curl -s -H "Authorization: Bearer $PROTON_TOOLS_API_KEY" \
       http://proton-mail-tools:8930/mailboxes
   ```

6. **Register in OpenWebUI**: *Admin Settings → Tools → Add Tool Server (OpenAPI)*
   - URL: `http://proton-mail-tools:8930`
   - Auth: Bearer, value of `PROTON_TOOLS_API_KEY`

   OpenWebUI fetches `/openapi.json` and lists the tools. Enable them for a model
   (*Workspace → Models → Tools*) or per chat with the `+` button, then ask something like
   *"What unread mail did I get today?"* or *"Reply to the message from Alice saying I'll
   be there."*

### Updating

- **Tool service:** `git pull` then `docker compose up -d --build proton-mail-tools`
  (Bridge keeps running).
- **Bridge:** bump `BRIDGE_VERSION` in [`bridge/Dockerfile`](bridge/Dockerfile), then
  `docker compose up -d --build proton-bridge proton-mail-tools` (the tool container is
  recreated because it shares the namespace). Bridge's own auto-updater is irrelevant
  inside the container.
- Both images are built locally, so a stack-wide `docker compose pull` reports them as
  skipped (use `docker compose pull --ignore-buildable` to silence any warning).

### Backup

Everything Bridge needs is on the `proton-bridge-data` volume; the mail cache can always
be re-synced, the vault and keychain cannot. `docker run --rm -v <project>_proton-bridge-data:/data
-v "$PWD":/backup alpine tar czf /backup/proton-bridge-data.tgz -C /data .` captures both.

## Configuration

Set on the `proton-mail-tools` container (the compose files map them from `PROTON_*`
variables in `.env`).

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `:8930` | HTTP bind address inside the shared namespace |
| `API_KEY` | *(empty = no auth)* | Bearer token OpenWebUI must send |
| `BRIDGE_IMAP_ADDR` | `127.0.0.1:1143` | Bridge IMAP endpoint |
| `BRIDGE_SMTP_ADDR` | `127.0.0.1:1025` | Bridge SMTP endpoint |
| `BRIDGE_USERNAME` | **required** | Proton address shown by Bridge `info` |
| `BRIDGE_PASSWORD` | **required** | Bridge-generated password (not your Proton password) |
| `BRIDGE_TLS_SKIP_VERIFY` | `true` | Bridge uses a self-signed certificate on loopback |
| `MAIL_FROM_ADDRESS` | `BRIDGE_USERNAME` | From address for outgoing mail (any address on the account) |
| `MAIL_FROM_NAME` | *(empty)* | From display name |
| `ALLOW_SEND` | `true` | Gate `send_message` / `reply_to_message` (403 when false) |
| `ALLOW_DELETE` | `true` | Gate `trash_messages` and `delete_mailbox` (403 when false) |
| `ALLOW_PURGE` | `false` | Gate `delete_messages` / `empty_mailbox` — permanent deletion in Spam and Trash |
| `MAX_BODY_CHARS` | `20000` | Truncate long bodies for the model |
| `SEARCH_DEFAULT_LIMIT` | `20` | Results when `limit` is omitted |
| `SEARCH_MAX_LIMIT` | `100` | Hard cap on `limit` |

`proton-bridge` takes `HOME` (default `/root`; see the migration guide) and nothing else.

## Security notes

- **The agent can read, send and delete your mail.** Treat the API key like a password,
  keep OpenWebUI itself behind authentication, and consider `ALLOW_SEND=false` /
  `ALLOW_DELETE=false` until you trust the model's tool use. Tool descriptions ask the
  model to confirm with the user before sending or trashing, but that is advice, not
  enforcement.
- Bridge's IMAP/SMTP ports and the tool port are not published to the host; only
  containers on the shared Docker network can reach `proton-mail-tools:8930`, and only
  with the bearer key.
- TLS verification towards Bridge is skipped because Bridge issues a self-signed
  certificate and the connection never leaves the container's loopback.
- With the default `ALLOW_PURGE=false`, deleting is always "move to Trash" and nothing is
  expunged. Enabling it adds `delete_messages` / `empty_mailbox`, which are irreversible
  but accept only the Spam and Trash mailboxes — the two places where Proton's own
  "Delete" is permanent — so the model cannot purge INBOX or a folder. Bridge itself, however,
  permanently deletes a message that is moved into Trash or Spam while it is already
  there — easy to trigger on Proton because one message can be in several mailboxes
  (INBOX and Sent for self-addressed mail, INBOX and a label). `trash_messages` and
  `move_messages` therefore check the destination by `Message-ID` first and report such
  uids in `skipped_uids` instead of moving them.
- Secrets live in `.env` (git-ignored) and the Bridge vault on the volume.

## Behaviour details

- `search_messages` defaults to `INBOX`; filters are ANDed; results are the newest
  `limit` matches by internal date, newest first (Bridge assigns UIDs in sync order, so
  UID order is not recency).
- `read_message` uses `BODY.PEEK` — the message stays unread until the agent calls
  `update_message_flags`. Plain-text parts are preferred; HTML is converted to text. Links
  are stripped from the body text but reported separately in `links` (first 50, with
  anchor text), and `List-Unsubscribe` header URLs in `list_unsubscribe`, so the agent can
  find e.g. an unsubscribe link.
- Organising: `move_messages` for folders (a Proton message lives in exactly one folder),
  `label_messages`/`unlabel_messages` for labels (additive; a message can carry many),
  `update_message_flags` with `flagged` for Proton's star, `archive_messages` /
  `trash_messages` for the system mailboxes. All of them look the message up by
  `Message-ID` in the destination first and report already-present / absent uids in
  `skipped_uids`.
- Mailbox management: `create_mailbox` takes a `kind` (`folder` or `label`) and a bare
  name and returns the exact `Folders/…` / `Labels/…` name; folders may be nested with
  `/`, labels may not. `rename_mailbox` keeps the kind. `delete_mailbox` removes a label
  (messages keep their folder) or an **empty** folder; a folder that still holds messages
  is refused so nothing is moved or deleted implicitly. System mailboxes are never
  renamed or deleted. Bridge pushes all three to Proton, so the change shows up in the
  web and mobile apps.
- `reply_to_message` replies to `Reply-To`/`From` (or to the original recipients when the
  original is your own message); `reply_all` adds the other recipients as Cc, excluding
  yourself. The original is not quoted. Bridge files sent mail into *Sent* itself.
- Errors are JSON `{"error": "..."}` with 400 (bad input), 401 (key), 403 (gated),
  404 (unknown mailbox/uid) or 502 (Bridge/Proton failure, message included).

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `bridge IMAP login: imap: NO no such user` | Username/Bridge-password pair is wrong for this Bridge instance. Use the exact values from `info` in the Bridge CLI; the Bridge password is ~22 chars, not your Proton password. |
| `too many login attempts` | Bridge rate-limits failed logins; wait a minute or two and fix the credentials first. |
| OpenWebUI shows "Connection error" for the tool server | OpenWebUI and `proton-bridge` are not on the same Docker network, or the alias is missing. Test with `docker exec <openwebui> curl http://proton-mail-tools:8930/health`. |
| Mailbox counts are low / old mail missing | Initial sync still running: `docker compose logs -f proton-bridge`. |
| `502 … Message does not exist` on move/trash | The message was removed by another client in between; search again. |
| `docker compose pull` says `pull access denied for proton-bridge` | Older compose files named the locally built image; remove the `image:` line from the `proton-bridge` service (current files have none) or run `docker compose pull --ignore-buildable`. |
| Bridge logs `no vault key found, generating new` on start | The `pass` keychain on the volume is missing or was reset; you must `init` → `login` again. |

Bridge writes detailed logs to `<HOME>/.local/share/protonmail/bridge-v3/logs/` on the
volume; the tool service logs one JSON line per request to stdout
(`docker compose logs -f proton-mail-tools`), which shows exactly which tools OpenWebUI
calls and with what status.

## Development

Go is not needed on the host; everything runs in the `golang:1.25` image:

```bash
docker run --rm -v gomodcache:/go/pkg/mod -v "$PWD":/src -w /src golang:1.25 \
  sh -c 'gofmt -l . && go vet ./... && go test ./...'
scripts/smoke.sh http://127.0.0.1:8930 "$PROTON_TOOLS_API_KEY"   # read-only checks against a running instance
```

```
bridge/                 headless Proton Mail Bridge image (pinned version, pass keychain, no exposed ports)
cmd/proton-mail-tools   main: wiring and HTTP server lifecycle
internal/config         environment → Config
internal/mail           domain types, Store/Sender ports, sentinel errors
internal/service        use cases and policy (permissions, limits, reply building, Trash guard)
internal/bridge         IMAP (go-imap v2) and SMTP (net/smtp) adapters for Bridge
internal/httpapi        handlers, bearer auth, embedded openapi.json (a test keeps it in sync with the routes)
internal/textconv       HTML → plain text
scripts/                smoke test, desktop-Bridge migration
docs/                   guides
```

## License

GNU General Public License v3.0 — see [LICENSE](LICENSE). Proton Mail Bridge is
downloaded from Proton at image build time and is subject to
[its own license](https://github.com/ProtonMail/proton-bridge/blob/master/LICENSE).
