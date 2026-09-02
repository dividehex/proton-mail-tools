# Migrating a desktop Bridge into the container

If Proton Mail Bridge is already installed and fully synced on the Docker host (Linux),
its state can be moved into the `proton-bridge` container so it does not have to sync
again. This is a **move**, not a copy: once the container owns the vault, the desktop
Bridge must not be started against that data again — two Bridge instances cannot share
one Proton session.

Three pieces of state are coupled and move together:

| What | Where (desktop, Linux) | Notes |
|---|---|---|
| Vault | `~/.config/protonmail/bridge-v3/vault.enc` | Account session, Bridge password, settings. Encrypted with a key held in the OS keychain. |
| Vault key | GNOME Keyring / KWallet (`secret-service`) or `pass` | Without it the vault is unreadable. |
| Mail cache + sync state | `~/.local/share/protonmail/bridge-v3/gluon`, `~/.config/protonmail/bridge-v3/imap-sync` | Encrypted with a key stored *inside* the vault. |

The vault records absolute paths, so the container must run with the same `HOME` as the
desktop user and mount the volume there. That is what `PROTON_BRIDGE_HOME` does.

## Prerequisites

- The container image version equals or exceeds the desktop Bridge version
  (`bridge/Dockerfile` → `BRIDGE_VERSION`; desktop: `protonmail-bridge --cli` then `>>> help`
  shows it in the banner). Older container + newer vault is not supported.
- Desktop keychain is `secret-service` (check `~/.config/protonmail/bridge-v3/keychain.json`)
  and `secret-tool` is installed (`sudo apt install libsecret-tools`). If the desktop Bridge
  uses the `pass-app` helper instead, the key is at
  `~/.password-store/docker-credential-helpers/<base64>/bridge-vault-key` and can be
  read with `pass show`.
- Desktop sync finished (`~/.local/share/protonmail/bridge-v3/logs/*.log` contains
  `Finished user sync`).

## Steps

1. In the stack's `.env`, set the desktop user's home and (re)create the containers so
   they pick it up:

   ```bash
   echo "PROTON_BRIDGE_HOME=$HOME" >> .env
   docker compose up -d proton-bridge proton-mail-tools
   ```

2. Export the vault key (this is a credential — keep the file private, delete it after):

   ```bash
   secret-tool lookup username bridge-vault-key > ~/.proton-vault-key
   chmod 600 ~/.proton-vault-key
   wc -c ~/.proton-vault-key        # 44
   ```

3. Quit the desktop Bridge (GUI: *Quit*; CLI: `Ctrl+C`).

4. Run the migration from the compose project directory:

   ```bash
   proton-mail-tools/scripts/migrate-host-bridge.sh ~/.proton-vault-key
   ```

   The script stops both containers, empties the volume, copies `vault.enc`, `imap-sync`
   and the gluon cache, creates the container's `pass` keychain and stores the key under
   the entry Bridge expects, then starts both containers.

5. Verify and clean up:

   ```bash
   docker exec <openwebui-container> curl -s -H "Authorization: Bearer $PROTON_TOOLS_API_KEY" \
       http://proton-mail-tools:8930/mailboxes
   rm ~/.proton-vault-key
   ```

   Counts should match the desktop Bridge immediately, and `docker compose logs
   proton-bridge` must not contain `no vault key found, generating new`.

`PROTON_BRIDGE_USERNAME` / `PROTON_BRIDGE_PASSWORD` stay as they were on the desktop —
the Bridge password travels with the vault.

## Afterwards

To use a desktop Bridge again on the same machine, delete
`~/.config/protonmail/bridge-v3` and `~/.local/share/protonmail/bridge-v3` and log in
fresh; it becomes a separate instance with its own Bridge password.

## Fallback

If anything fails, the container is in a clean state: run
`docker compose run --rm -it proton-bridge init`, `login`, `info`, and let it sync.
