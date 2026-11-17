# Character backups off the Pi

## Why

All characters live in one SQLite database on the Raspberry Pi's SD card (`/opt/dnd/data/dnd.db`).
SD cards die. Today nothing copies the database anywhere else, so a dead card loses every character.

## Goals

- Every night, a consistent copy of the database ends up on Sylvie's Mac.
- A dead SD card costs at most about a day of changes, or the time since the Mac was last awake.
- Restoring onto a freshly set-up Pi is written down and has been tried once.

Non-goals: cloud or off-site storage, restoring one character on its own, backing up `content/` (it
is in the repo, and deploy-pi.sh copies it to the Pi).

Anti-goals: a backup that only looks fine (a torn copy of a WAL database, or a job that stopped
months ago without anyone noticing); a backup path that needs sudo.

## Constraints (as found on the Pi)

- `dndserver` runs as user `dnd` under systemd with `ProtectSystem=strict`, and writes only to
  `/opt/dnd/data`.
- The database is mode 0600, owned by `dnd`, in WAL mode. A plain file copy is not safe.
- The Mac already logs into `sylvie@slvie-on-pi.local` with a key.

## Approaches

1. **The Mac pulls the snapshot over SSH (chosen).** It needs no new account and keeps credentials
   off the Pi, and it builds on the SSH setup deploy-pi.sh already uses. Backups only run while
   the Mac is awake. launchd runs a missed job when the Mac wakes.
2. The Pi pushes to cloud storage with rclone. It runs unattended and keeps a copy outside the
   house, but it needs another account and a credential stored on the Pi.
3. A USB stick on the Pi. It sits in the same place and on the same power as the Pi, so whatever
   kills the Pi can kill the backup too.

## Design

**`dnd-admin backup <dir>`** (new Go subcommand in `cmd/admin`, tested). It opens the database as
the other subcommands do and runs `VACUUM INTO` to a temporary file in `<dir>`. Then it runs
`PRAGMA integrity_check` on that file and renames it to `dnd-YYYY-MM-DD.db` (mode 0640). After
that it deletes all but the newest 7 `dnd-*.db` files in `<dir>`. If any step fails, it exits
non-zero and leaves no partial file.

**On the Pi:** a systemd service and timer (`dnd-backup.service`/`.timer`) run the command nightly
as `dnd`, with `Persistent=true`, writing to `/opt/dnd/backups`. That folder is owned by
`dnd:sylvie` with mode 2750, so `sylvie` can read the snapshots but not change them.

**On the Mac:** a script, run by a launchd agent each night and on wake, downloads the newest
snapshot. It saves it to `~/Backups/dnd/` and keeps 30 days. It runs `PRAGMA integrity_check`
on the copy, using the `sqlite3` that ships with macOS. It logs to `~/Backups/dnd/backup.log`.
If the newest good copy is more than 3 days old, it shows a macOS notification.

**SSH key:** the pull uses the Mac's existing `~/.ssh/id_rsa`, which has no passphrase, so the
launchd job can use it without the keychain. Sylvie decided against a separate backup-only key.

**Restore checklist** (`docs/restore-from-backup.md`): flash the card, create the `dnd` user,
`/opt/dnd`, the systemd unit and `/etc/dnd.env` (the secrets come from the Mac's `.env`, not from
the backup), then run deploy-pi.sh. Copy a snapshot to `/opt/dnd/data/dnd.db`, owned by `dnd` with
mode 0600, and start the service. Test it once: restore a snapshot into a local `data/` and check
that the characters load.

## Left to the builder (my calls; tell me if any are wrong)

- `VACUUM INTO` over the online-backup API: it needs nothing beyond SQL in the modernc driver, and
  it produces a compact file.
- The Pi-side unit files and the Mac script live in `scripts/` and are gitignored, as
  deploy-pi.sh is, because they name your host. The Go command and the restore doc are committed.
- The retention numbers (7 on the Pi, 30 on the Mac) and the 3-day warning.
- The installation on the Pi needs sudo once. It goes in a setup script you run yourself.
