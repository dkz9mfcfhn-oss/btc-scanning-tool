# MineWatch cloud watchdog

This stdlib-only Python service is paired with the existing self-hosted Bark bridge. The supplied installer targets the existing `/opt/rainline` Caddy deployment and `rainline-https.service`; review those paths before installing on another server.

Place `watchdog.py` at `/tmp/minewatch-watchdog.py`, then run `sh install.sh` as the server administrator. The installer backs up and validates a changed Caddyfile before restarting HTTPS. This service listens only on loopback port 8790. The existing HTTPS proxy routes `/minewatch-watchdog/*` to it.

The desktop sends its random persistent monitor ID and enabled/paused flags with the existing Bark key in the Authorization header, to the same self-hosted host. No miner IPs, worker names or notes are uploaded. State is stored under the protected systemd StateDirectory; the service does not print capability keys or request bodies.

Heartbeats: immediately, then 10 minutes. Missing-heartbeat threshold: more than 25 minutes. Startup grace: 11 minutes. One outage notification, then one recovery notification; failed notifications retry after one minute. The desktop can explicitly disarm watch before maintenance. Stopping the cloud server itself prevents it from delivering alerts.

Run `python3 -m unittest discover -s cloud -p 'test_*.py'` from the source root. Tests use a fake sender and never send phone notifications.
