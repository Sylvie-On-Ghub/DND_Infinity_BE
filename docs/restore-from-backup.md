# Restoring the Pi from a backup

Every night at about 03:30 the Pi writes a checked snapshot of the database to `/opt/dnd/backups`
(`dnd-backup.timer`, which keeps 7). Later, the Mac copies the newest one into `~/Backups/dnd/`, which
keeps 30 days. `~/Backups/dnd/backup.log` has one line per pull. The Mac shows a notification when its
newest copy is more than 3 days old.

The scripts these steps use are gitignored, as `scripts/deploy-pi.sh` is: `scripts/setup-pi-backup.sh`,
`scripts/pi-backup/`, `scripts/pull-pi-backup.sh` and `scripts/install-mac-backup.sh`.

Secrets are not in the backup. `/etc/dnd.env` comes from your own `.env`, and the Cloudflare tunnel
token comes from the Cloudflare dashboard.

## If the SD card dies

1. **Flash a new card** with Raspberry Pi OS Lite (64-bit). In Raspberry Pi Imager, set the
   hostname `slvie-on-pi` and user `sylvie`, and turn on SSH with the Mac's `~/.ssh/id_rsa.pub`.

2. **Forget the old Pi's host key on the Mac, then connect once by hand** so the new one is
   accepted. Until you do, the nightly pull fails.

   ```sh
   ssh-keygen -R slvie-on-pi.local
   ssh sylvie@slvie-on-pi.local true
   ```

3. **Create the service user and folders** on the Pi:

   ```sh
   sudo useradd --system --home /opt/dnd --shell /usr/sbin/nologin dnd
   sudo install -d -o root -g root -m 755 /opt/dnd
   sudo install -d -o dnd -g dnd -m 755 /opt/dnd/data
   ```

4. **Recreate `/etc/dnd.env`** (owned by root, mode 600). Start from `.env.example`, take
   `DISCORD_BOT_TOKEN` and `DND_DISCORD_ADMIN_IDS` from your `.env`, and use the Pi's values:

   ```sh
   DND_DB_PATH=/opt/dnd/data/dnd.db
   DND_LISTEN_ADDR=127.0.0.1:8080
   DND_CORS_ORIGINS=https://app.transdev.me.uk
   DND_DEV_MODE=0
   DND_TRUSTED_PROXY_HEADER=CF-Connecting-IP
   ```

5. **Recreate the server unit** as `/etc/systemd/system/dnd.service`, then run
   `sudo systemctl daemon-reload && sudo systemctl enable dnd`:

   ```ini
   [Unit]
   Description=DND API
   After=network-online.target
   Wants=network-online.target

   [Service]
   User=dnd
   WorkingDirectory=/opt/dnd
   EnvironmentFile=/etc/dnd.env
   ExecStart=/opt/dnd/dndserver
   Restart=on-failure
   RestartSec=5
   NoNewPrivileges=true
   ProtectSystem=strict
   ProtectHome=true
   PrivateTmp=true
   ReadWritePaths=/opt/dnd/data

   [Install]
   WantedBy=multi-user.target
   ```

6. **Deploy** from the Mac: `scripts/deploy-pi.sh`. This starts the server on an empty database.

7. **Put the backup in place.** Copy the newest snapshot from the Mac, then on the Pi stop the
   server, swap the database in and start the server again:

   ```sh
   # on the Mac
   scp "$(ls ~/Backups/dnd/dnd-*.db | sort | tail -n1)" sylvie@slvie-on-pi.local:dnd-restore.db

   # on the Pi
   sudo systemctl stop dnd
   sudo rm -f /opt/dnd/data/dnd.db-wal /opt/dnd/data/dnd.db-shm
   sudo install -o dnd -g dnd -m 600 ~/dnd-restore.db /opt/dnd/data/dnd.db
   sudo systemctl start dnd
   ```

8. **Check the characters are back:**

   ```sh
   sudo -u dnd env DND_DB_PATH=/opt/dnd/data/dnd.db /opt/dnd/dnd-admin list-users
   ```

9. **Reinstall the Cloudflare tunnel.** Install `cloudflared`, then run
   `sudo cloudflared service install <token>`. The token is in the Cloudflare dashboard, under
   Zero Trust → Networks → Tunnels → your tunnel.

10. **Turn backups back on:** run `scripts/setup-pi-backup.sh` from the Mac. The Mac side needs no
    changes.

## Checking a backup without touching the Pi

Any snapshot can be opened on the Mac:

```sh
cp "$(ls ~/Backups/dnd/dnd-*.db | sort | tail -n1)" /tmp/dnd-check.db
DND_DB_PATH=/tmp/dnd-check.db go run ./cmd/admin list-users
```

The users and character counts should match those on the Pi.
