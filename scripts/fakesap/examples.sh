#!/bin/sh
# Transcripts of the real kernelman binary on the fake SAP host (root only):
# docs/examples/fakesap/*.txt and .png. Builds the host from scratch, so it
# refuses to run where a real SAP system exists (see setup.sh).
set -eu
cd "$(dirname "$0")/../.."
out=docs/examples/fakesap
mkdir -p "$out" && rm -f "$out"/*.txt "$out"/*.png
install -m 755 bin/kernelman /usr/local/bin/kernelman
SETTLE=6 STARTSERVICE=fail scripts/fakesap/setup.sh >/dev/null
mkdir -p /usr/sap/download && cp -p /home/tayfur/kernel/*_4* /usr/sap/download/ && chown -R tayfur /usr/sap/download && chmod 644 /usr/sap/download/*
C='KERNELMAN_COLOR=always KERNELMAN_UNICODE=1'
calls() { grep -E "$1" /usr/sap/.fakesap/calls.log | sed 's/^/  /'; }

: > /usr/sap/.fakesap/calls.log
{ echo '$ su - devadm -c "kernelman stop"        # sahte SAP hostu (scripts/fakesap), gerçek ikili, devadm = tcsh'
  su - devadm -c "env $C kernelman stop" 2>&1
  echo; echo '$ grep -E "StopSystem|StopService" /usr/sap/.fakesap/calls.log    # SAP durdu, sapstartsrv durdurulmadı'
  calls "StopSystem|StopService"; } > "$out/stop-keeps-sapstartsrv.txt"

: > /usr/sap/.fakesap/calls.log
{ echo '$ su - devadm -c "kernelman update --yes --from /usr/sap/download"   # sidadm, root sahipli sapuxuserchk, StartService bozuk'
  su - devadm -c "env $C kernelman update --yes --from /usr/sap/download" 2>&1 | grep -v "^    -r\|^    d\|^    total\|more entries"
  echo; echo '$ grep -E "Stop|Start|SAPCAR" /usr/sap/.fakesap/calls.log'
  calls "Service|SAPCAR -xvf" | head -8; echo '  …'; calls "StartService" ; } > "$out/update-sidadm.txt"

cp /usr/sap/DEV/ASCS01/exe_*/disp+work /usr/sap/DEV/ASCS01/exe/disp+work
{ echo '$ cp exe_<tarih>/disp+work /usr/sap/DEV/ASCS01/exe/   # ASCS01 eski seviyede kaldı'
  echo '$ kernelman start </dev/null                         # otomatik (hub gibi): başlatmaz'
  env KERNELMAN_COLOR=always KERNELMAN_UNICODE=1 kernelman start </dev/null 2>&1 || true; } > "$out/start-refuses-old-instance.txt"

for f in "$out"/*.txt; do python3 scripts/screen2png.py "$f" "${f%.txt}.png" "kernelman — fakesap/$(basename "${f%.txt}")" >/dev/null; done
ls "$out"
