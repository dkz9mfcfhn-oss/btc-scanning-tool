#!/bin/sh
set -eu
install -d -m 755 /opt/minewatch-watchdog
install -m 644 /tmp/minewatch-watchdog.py /opt/minewatch-watchdog/watchdog.py
cat > /etc/systemd/system/minewatch-watchdog.service <<'UNIT'
[Unit]
Description=MineWatch cloud heartbeat watchdog
After=network-online.target minewatch-bark.service
Wants=network-online.target
[Service]
Type=simple
DynamicUser=yes
StateDirectory=minewatch-watchdog
StateDirectoryMode=0700
ExecStart=/usr/bin/python3 /opt/minewatch-watchdog/watchdog.py
Restart=on-failure
RestartSec=5
MemoryMax=64M
CPUQuota=10%
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
[Install]
WantedBy=multi-user.target
UNIT
python3 - <<'PY'
from pathlib import Path
import subprocess
p=Path('/opt/rainline/deploy/Caddyfile');s=p.read_text()
if 'handle /minewatch-watchdog/*' not in s:
 needle='    handle /minewatch-bark/* {'
 assert s.count(needle)==1
 candidate=s.replace(needle,'    handle /minewatch-watchdog/* {\n        reverse_proxy 127.0.0.1:8790\n    }\n'+needle)
 test=Path('/opt/minewatch-watchdog/Caddyfile.candidate');test.write_text(candidate)
 subprocess.run(['/opt/rainline/caddy','validate','--config',str(test),'--adapter','caddyfile'],check=True)
 Path('/opt/minewatch-watchdog/Caddyfile.before').write_text(s)
 p.write_text(candidate)
 subprocess.run(['systemctl','restart','rainline-https.service'],check=True)
PY
systemd-analyze verify /etc/systemd/system/minewatch-watchdog.service
systemctl daemon-reload
systemctl enable --now minewatch-watchdog.service
systemctl is-active minewatch-watchdog.service
curl --fail --retry 5 --retry-connrefused --retry-delay 2 --max-time 5 http://127.0.0.1:8790/minewatch-watchdog/health
