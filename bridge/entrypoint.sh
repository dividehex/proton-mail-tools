#!/bin/bash
# Usage: entrypoint.sh            -> run Bridge headless (default)
#        entrypoint.sh init|cli   -> interactive Bridge CLI (first login, `info`, etc.)
set -e

# Bridge keeps its vault key in a keychain; in the container that is `pass`,
# backed by a passphrase-less GPG key created on first run.
if ! gpg --batch --list-secret-keys pass-key >/dev/null 2>&1; then
    gpg --batch --generate-key /protonmail/gpgparams
fi
if [ ! -d "$HOME/.password-store" ]; then
    pass init pass-key
fi

case "${1:-}" in
    init|cli) exec protonmail-bridge --cli ;;
    *)        exec protonmail-bridge --noninteractive "$@" ;;
esac
