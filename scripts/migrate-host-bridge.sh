#!/usr/bin/env bash
# Migrate a fully-synced desktop Proton Mail Bridge (Linux) into the proton-bridge
# container so it does not have to re-sync. Run from the compose project directory.
#
# Usage: scripts/migrate-host-bridge.sh <vault-key-file> [host-home]
#   vault-key-file  raw secret from:  secret-tool lookup username bridge-vault-key > file
#   host-home       home dir of the desktop Bridge user (default: $HOME)
#
# Preconditions: the desktop Bridge is NOT running, and it must not be started again
# against this vault afterwards (two Bridge instances cannot share one session).
set -euo pipefail

KEYFILE="${1:?vault key file required}"
HOST_HOME="${2:-$HOME}"
VOLUME="${VOLUME:-$(docker compose config --format json | python3 -c 'import json,sys; v=json.load(sys.stdin)["volumes"]["proton-bridge-data"]; print(v.get("name") or "proton-bridge-data")')}"
# Build-only services carry no image name in the resolved config; Compose names them <project>-<service>.
IMAGE="$(docker compose config --format json | python3 -c 'import json,sys; c=json.load(sys.stdin); print(c["services"]["proton-bridge"].get("image") or c["name"]+"-proton-bridge")')"
CHOME="$(docker compose config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["proton-bridge"]["environment"].get("HOME","/root"))')"
CONF="$HOST_HOME/.config/protonmail/bridge-v3"
CACHE="$HOST_HOME/.local/share/protonmail/bridge-v3/gluon"
ENTRY="docker-credential-helpers/$(printf 'protonmail/bridge-v3/users/bridge-vault-key' | base64 -w0)/bridge-vault-key"

[[ -s "$KEYFILE" ]] || { echo "key file is empty" >&2; exit 1; }
[[ -f "$CONF/vault.enc" && -d "$CACHE" ]] || { echo "no Bridge state under $HOST_HOME" >&2; exit 1; }
[[ "$CHOME" == "$HOST_HOME" ]] || { echo "container HOME is $CHOME but the desktop Bridge lived in $HOST_HOME; set PROTON_BRIDGE_HOME=$HOST_HOME in .env first" >&2; exit 1; }
# Desktop Bridge runs as `bridge -c` (CLI) or via bridge-gui; the container's is `bridge --noninteractive`.
if pgrep -f '^/usr/lib/protonmail/bridge/(bridge -c|bridge-gui|bridge --grpc)' >/dev/null 2>&1; then
    echo "desktop Bridge appears to be running; quit it first" >&2; exit 1
fi

echo ">> stopping containers"
docker compose stop proton-mail-tools proton-bridge

echo ">> resetting volume $VOLUME and copying host state ($(du -sh "$CACHE" | cut -f1) cache)"
docker run --rm -i \
    -v "$VOLUME:$CHOME" \
    -v "$CONF:/src/conf:ro" -v "$CACHE:/src/gluon:ro" -v "$KEYFILE:/src/vault-key:ro" \
    -e HOME="$CHOME" -e ENTRY_PATH="$ENTRY" --entrypoint bash "$IMAGE" -s <<'INNER'
set -euo pipefail
cd "$HOME"
find . -mindepth 1 -delete
mkdir -p .config/protonmail/bridge-v3 .local/share/protonmail/bridge-v3
cp -a /src/conf/vault.enc /src/conf/imap-sync .config/protonmail/bridge-v3/
printf '{\n  "Helper": "pass-app",\n  "DisableTest": false\n}\n' > .config/protonmail/bridge-v3/keychain.json
printf '{\n  "FailedAttempts": 0\n}\n' > .config/protonmail/bridge-v3/keychain_state.json
cp -a /src/gluon .local/share/protonmail/bridge-v3/gluon
gpg --batch --quiet --generate-key /protonmail/gpgparams 2>/dev/null
pass init pass-key >/dev/null
printf "%s" "$(cat /src/vault-key)" | pass insert -f -m "$ENTRY_PATH" >/dev/null   # key is base64, no newline
echo "   vault key stored at $ENTRY_PATH; $(find .local/share/protonmail/bridge-v3/gluon -type f | wc -l) cache files copied"
INNER

echo ">> starting containers"
docker compose up -d proton-bridge proton-mail-tools
echo ">> done; watch: docker compose logs -f proton-bridge"
