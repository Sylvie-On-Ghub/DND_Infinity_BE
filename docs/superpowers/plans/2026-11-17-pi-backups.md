# Pi Backups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Snapshot the Pi's character database nightly and pull each snapshot to the Mac, so a dead SD card loses at most about a day.

**Architecture:** A new `character.BackupDB` writes a verified `VACUUM INTO` snapshot and prunes old ones. `dnd-admin backup <dir>` exposes it. A systemd timer on the Pi runs it as `dnd` into `/opt/dnd/backups`. A launchd agent on the Mac pulls the newest snapshot over SSH by running a send-latest script on the Pi, verifies it and keeps 30 days.

**Tech Stack:** Go (package `character` at repo root, modernc sqlite), bash, systemd, launchd, macOS `sqlite3`.

**Spec:** `docs/superpowers/specs/2026-11-17-pi-backups-design.md`

## Global Constraints

- Work on a branch `pi-backups`, not on `main`.
- Snapshot file names: `dnd-YYYY-MM-DD.db` (local date), mode 0640. Temporary files start with `.` so pruning and pulling never see them.
- Keep 7 snapshots on the Pi and 30 days on the Mac. Warn when the newest good Mac copy is more than 3 days old.
- Pi: `/opt/dnd/backups` is owned `dnd:sylvie` with mode 2750. The backup runs as `dnd`. Neither the backup nor the pull needs sudo.
- Mac: snapshots go in `~/Backups/dnd/` and the log in `~/Backups/dnd/backup.log`. SSH uses the Mac's existing key (`~/.ssh/id_rsa`, no passphrase). No separate backup key.
- Host default `sylvie@slvie-on-pi.local`, overridable with `DND_PI_HOST`, as in deploy-pi.sh.
- These files are gitignored, as deploy-pi.sh is: `scripts/setup-pi-backup.sh`, `scripts/pi-backup/`, `scripts/pull-pi-backup.sh`, `scripts/install-mac-backup.sh`. Committed: Go code, tests, `docs/restore-from-backup.md`.
- Secrets (`/etc/dnd.env`) are never backed up.

## Review Focus

1. The `DND_DB_PATH` is wrong or the file is missing: `OpenDB` would silently create an empty database and back that up. `backup` must refuse with an error and create nothing (Task 2 `TestBackupMissingDatabase`).
2. The backup runs twice on one day (e.g. timer plus manual run): the second replaces that day's file and doesn't fail on "file exists" (Task 1 `TestBackupDBSameDayReplaces`).
3. Writes are happening while the snapshot is taken (WAL has uncommitted pages): the snapshot holds every committed row (Task 1 `TestBackupDBIncludesWALRows`).
4. The backup folder has other files or leftover temp files: pruning only touches `dnd-YYYY-MM-DD.db` files (Task 1 `TestBackupDBPrunesOnlySnapshots`).
5. The Pi is unreachable or the pull gets a truncated file: the Mac keeps its existing copies, writes no bad file, logs the failure and still runs the staleness warning (Task 4 Step 4).

---

### Task 1: `BackupDB`

**Files:**
- Create: `backup.go`
- Test: `backup_test.go`

**Interfaces:**
- Produces: `func BackupDB(ctx context.Context, db *sql.DB, dir string, keep int, now time.Time) (string, error)`. It returns the path of the snapshot it wrote.

- [ ] **Step 1: Write the failing tests** in `backup_test.go`. Use `OpenDB` on a `t.TempDir()` path, add a user and a character with the existing helpers (as `store_test.go` does), and open snapshots with `sql.Open("sqlite", "file:"+p+"?mode=ro")`.
  - `TestBackupDBWritesVerifiedSnapshot`: with `now = 2026-11-17`, the returned path is `<dir>/dnd-2026-11-17.db`. Its mode is `0640`. `SELECT count(*) FROM characters` = 1. The directory has no file starting with `.`.
  - `TestBackupDBIncludesWALRows`: keep the source db open, insert a second character, back up without closing; the snapshot has 2 characters.
  - `TestBackupDBSameDayReplaces`: back up twice with the same `now`, adding a character in between; no error, one file, and it has the newer count.
  - `TestBackupDBPrunesOnlySnapshots`: pre-create `dnd-2026-11-01.db` … `dnd-2026-11-10.db` (empty files), `notes.txt` and `.dnd-old.tmp`. Back up with `keep=7` and `now=2026-11-17`. The remaining `dnd-*.db` files are 11-05 … 11-10 and 11-17. `notes.txt` and `.dnd-old.tmp` are still there.
  - `TestBackupDBMissingDir`: `dir` doesn't exist; an error, and nothing created.

- [ ] **Step 2: Run them and see them fail**

Run: `go test -run TestBackupDB .`
Expected: FAIL, `undefined: BackupDB`

- [ ] **Step 3: Implement `BackupDB` in `backup.go`.** Open with a header comment in the style of `db.go`. Steps:
  1. Choose a temp name `.dnd-backup-<unix nanos>.db` in `dir`. Don't create the file, because `VACUUM INTO` refuses an existing one.
  2. `VACUUM INTO ?` with the temp path.
  3. Open the temp file read-only and run `PRAGMA integrity_check`. Anything other than a single `ok` is an error.
  4. `chmod 0640`, then rename to `dnd-<now.Format(time.DateOnly)>.db`.
  5. Prune: match the names with `^dnd-\d{4}-\d{2}-\d{2}\.db$`, sort them, and delete all but the last `keep`.

  On any error before the rename, remove the temp file. A missing `dir` is an error; `BackupDB` doesn't create it.

- [ ] **Step 4: Run the tests**

Run: `go test -run TestBackupDB . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backup.go backup_test.go
git commit -m "Add BackupDB for verified database snapshots"
```

### Task 2: `dnd-admin backup <dir>`

**Files:**
- Modify: `cmd/admin/main.go` (usage, `wantArgs`, the switch)
- Test: `cmd/admin/main_test.go` (new)

**Interfaces:**
- Consumes: `character.BackupDB` (Task 1).
- Produces: the CLI `dnd-admin backup <dir>`. On success it prints `backed up to <path>` and exits 0.

- [ ] **Step 1: Write the failing tests** in `cmd/admin/main_test.go`, calling `run(ctx, args)` with `t.Setenv("DND_DB_PATH", …)`.
  - `TestBackupMissingDatabase`: `DND_DB_PATH` points to a file that doesn't exist. `run(ctx, []string{"backup", dir})` returns an error mentioning the path. Afterwards the database file still doesn't exist and `dir` is empty.
  - `TestBackupCommand`: create the database with `character.OpenDB` and add a user. `run(ctx, []string{"backup", dir})` returns nil, and `dir` has exactly one `dnd-*.db`.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./cmd/admin`
Expected: FAIL (usage error for an unknown command)

- [ ] **Step 3: Implement.** Add `backup <dir>` to the usage text, and `"backup": 1` to `wantArgs`. Before `OpenDB`, the `backup` command must `os.Stat(path)` and return `fmt.Errorf("no database at %s; check DND_DB_PATH", path)` if it's missing. Call `character.BackupDB(ctx, db, args[0], 7, time.Now())`. Update the package doc comment.

- [ ] **Step 4: Run the tests**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/admin/main.go cmd/admin/main_test.go
git commit -m "Add dnd-admin backup"
```

### Task 3: Nightly snapshot on the Pi

**Files (all gitignored; add to `.gitignore` under the deploy-script entry):**
- Create: `scripts/pi-backup/dnd-backup.service`: `Type=oneshot`, `User=dnd`, `EnvironmentFile=/etc/dnd.env`, `ExecStart=/opt/dnd/dnd-admin backup /opt/dnd/backups`, the same hardening as the server unit (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`), and `ReadWritePaths=/opt/dnd/data /opt/dnd/backups`. The data path needs write access for the WAL `-shm` file.
- Create: `scripts/pi-backup/dnd-backup.timer`: `OnCalendar=*-*-* 03:30`, `Persistent=true`, `RandomizedDelaySec=10m`, `WantedBy=timers.target`.
- Create: `scripts/pi-backup/send-latest-backup`: bash. It finds the lexically last `dnd-????-??-??.db` in `/opt/dnd/backups` and runs `tar -C /opt/dnd/backups -cf - <name>`. With no snapshot it exits 1 and writes a message to stderr.
- Create: `scripts/setup-pi-backup.sh`: run from the Mac, in the style of deploy-pi.sh (`DND_PI_HOST`, staging in `~/dnd-deploy`, one `ssh -t` with sudo). On the Pi it creates `/opt/dnd/backups` (`dnd:sylvie`, 2750) and installs `send-latest-backup` to `/opt/dnd/` (root, 755). It installs the units into `/etc/systemd/system/`, then runs `daemon-reload`, `enable --now dnd-backup.timer` and `start dnd-backup.service`. Then it lists the folder. The script is safe to run again.

- [ ] **Step 1: Check prerequisites.** Run `scripts/deploy-pi.sh`, so the Pi has the `dnd-admin` from Task 2. Expected: `healthy: …`.
- [ ] **Step 2: Sylvie runs `scripts/setup-pi-backup.sh`** (it asks for the sudo password). Expected: the listing shows `dnd-<today>.db`, `-rw-r----- dnd sylvie`.
- [ ] **Step 3: Verify.** Run `ssh sylvie@slvie-on-pi.local 'systemctl list-timers dnd-backup.timer; /opt/dnd/send-latest-backup | tar -tf -'`. Expected: the next run is around 03:30, and `dnd-<today>.db` is listed. Running the service a second time the same day still leaves one file for today.
- [ ] **Step 4: Commit** only `.gitignore`: `git commit -m "Ignore the Pi backup scripts"`.

### Task 4: Pull to the Mac

**Files (gitignored):**
- Create: `scripts/pull-pi-backup.sh`: bash 3.2-compatible (macOS). It runs `ssh -o BatchMode=yes -o ConnectTimeout=20 "$HOST" /opt/dnd/send-latest-backup` into a temp dir under `~/Backups/dnd/.incoming`. It checks that the tar holds exactly one `dnd-YYYY-MM-DD.db`, runs `sqlite3 <file> 'PRAGMA integrity_check'` and requires `ok`, then moves it into `~/Backups/dnd/`. It deletes `dnd-*.db` whose name date is more than 30 days old. Every run appends one timestamped line to `backup.log`: `ok <name>` or `FAILED <reason>`. On any failure it leaves the existing files alone and cleans up `.incoming`. **Always last** it runs the staleness check: if the newest `dnd-*.db` (by name) is more than `${DND_BACKUP_MAX_AGE_DAYS:-3}` days old, or none exists, it shows `osascript -e 'display notification "Newest character backup is from <date>" with title "DND backup"'`. It exits non-zero on failure.
- Create: `scripts/install-mac-backup.sh`. It writes `~/Library/LaunchAgents/uk.transdev.dnd-backup.plist`, which runs the pull script at 09:00 daily (`StartCalendarInterval`, so a missed run happens on wake) and with `RunAtLoad`. stdout and stderr go to `~/Backups/dnd/launchd.log`. It runs `launchctl bootout` (ignoring errors), then `bootstrap gui/$(id -u)`.
- Modify: `.gitignore` (both scripts).

- [ ] **Step 1:** Run `scripts/install-mac-backup.sh`.
- [ ] **Step 2: Check that the background job can use the key.** Run `launchctl kickstart gui/$(id -u)/uk.transdev.dnd-backup`, then `tail -1 ~/Backups/dnd/backup.log`. Expected: `ok dnd-<today>.db`. If it fails with an auth error, the agent isn't getting the key, so pass `-i ~/.ssh/id_rsa` explicitly.
- [ ] **Step 3: Pull.** Run `scripts/pull-pi-backup.sh`. Expected: exit 0, `~/Backups/dnd/dnd-<today>.db` exists, and the last line of `backup.log` is `ok dnd-<today>.db`. `launchctl print gui/$(id -u)/uk.transdev.dnd-backup` shows the job loaded.
- [ ] **Step 4: Check the failure paths.**
  - `DND_PI_HOST=nobody@invalid.local scripts/pull-pi-backup.sh`: non-zero, a `FAILED` line in the log, and the existing files untouched.
  - `DND_BACKUP_MAX_AGE_DAYS=-1 scripts/pull-pi-backup.sh`: a notification appears.
  - Feed it a truncated tar: temporarily point `ssh` at a stub that runs `head -c 5000` of a real tar. The integrity check fails and no file is added.
- [ ] **Step 5: Commit** `.gitignore`: `git commit -m "Ignore the Mac backup scripts"`.

### Task 5: Restore checklist, tried once

**Files:**
- Create: `docs/restore-from-backup.md`

- [ ] **Step 1: Write the checklist.** Cover these steps, each with its command:
  1. Flash Raspberry Pi OS to a new card. Set user `sylvie` and hostname `slvie-on-pi`, and turn on SSH with the Mac's key.
  2. `sudo useradd --system --home /opt/dnd dnd`, then create `/opt/dnd` and `/opt/dnd/data` (owned `dnd`).
  3. Recreate `/etc/dnd.env` (mode 600, root) from the Mac's `.env` and the values in `.env.example`. Recreate the server unit. Copy its text into the doc from the current Pi; it is shown in the spec's Constraints section.
  4. Run `scripts/deploy-pi.sh`.
  5. `sudo systemctl stop <service>`. Copy the newest `~/Backups/dnd/dnd-*.db` to `/opt/dnd/data/dnd.db` (`chown dnd:dnd`, `chmod 600`), remove any `dnd.db-wal`/`-shm`, then start the service.
  6. The new Pi has a new host key, so first remove the old one with `ssh-keygen -R slvie-on-pi.local` and connect once by hand to accept the new one. Otherwise the Mac's background pull fails. Then run `scripts/setup-pi-backup.sh` again.
  7. Check: `sudo -u dnd env $(sudo cat /etc/dnd.env | xargs) /opt/dnd/dnd-admin list-users` shows the expected character counts.
- [ ] **Step 2: Try a restore locally.** Copy the newest Mac snapshot to the scratch folder, then run `DND_DB_PATH=<copy> go run ./cmd/admin list-users`. Expected: the same users and character counts as `ssh sylvie@slvie-on-pi.local 'sudo -u dnd env DND_DB_PATH=/opt/dnd/data/dnd.db /opt/dnd/dnd-admin list-users'`.
- [ ] **Step 3: Commit**

```bash
git add docs/restore-from-backup.md docs/superpowers/specs/2026-11-17-pi-backups-design.md docs/superpowers/plans/2026-11-17-pi-backups.md
git commit -m "Document restoring the Pi from a backup"
```
