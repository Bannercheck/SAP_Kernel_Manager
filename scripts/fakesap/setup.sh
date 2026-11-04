#!/bin/sh
# Build a fake SAP host (root only) to run the real kernelman agent and hub
# against: /usr/sap + /sapmnt layout, profiles, sapservices, SAP Host Agent,
# an ASCS and a dialog instance, today's archives in a 700 home directory.
# Every SAP program is a script sourcing lib.sh. Test tool: it removes
# /usr/sap and /sapmnt, so it refuses to run where a real SAP system exists.
#   SID=DEV DLUSER=tayfur REMOTE="vmapp2:10" STARTSERVICE=ok|fail SETTLE=8 scripts/fakesap/setup.sh
set -eu
[ "$(id -u)" = 0 ] || { echo "setup.sh: run as root" >&2; exit 1; }
if [ -e /usr/sap ] && [ ! -e /usr/sap/.fakesap/conf ]; then echo "setup.sh: /usr/sap exists and is not a fake host — refusing" >&2; exit 1; fi
SID=${SID:-DEV}
SIDADM=$(echo "$SID" | tr 'A-Z' 'a-z')adm
HOST=$(hostname)
DLUSER=${DLUSER:-tayfur}
here=$(cd "$(dirname "$0")" && pwd)
FS=/usr/sap/.fakesap
CT_RUN=/usr/sap/$SID/SYS/exe/uc/linuxx86_64
EXES="disp+work gwrd icman icmbnd sapuxuserchk msg_server enq_server enq_replicator sapstartsrv sapcontrol sapcpe saproot.sh R3trans tp"
LIBS="libsapu16.so libicuuc.so.50 dbhdbslib.so"

getent group sapsys >/dev/null || groupadd sapsys
id "$SIDADM" >/dev/null 2>&1 || useradd -m -s /usr/bin/tcsh "$SIDADM"
usermod -g sapsys "$SIDADM"
id "$DLUSER" >/dev/null 2>&1 || useradd -m "$DLUSER"
rm -rf /usr/sap /sapmnt
mkdir -p "$FS/state" /sapmnt/"$SID"/exe/uc/linuxx86_64 /sapmnt/"$SID"/profile /usr/sap/"$SID"/SYS/exe /usr/sap/hostctrl/exe /usr/sap/trans
ln -s /sapmnt/"$SID"/exe/uc /usr/sap/"$SID"/SYS/exe/uc
ln -s /sapmnt/"$SID"/profile /usr/sap/"$SID"/SYS/profile
ln -s "$CT_RUN" /usr/sap/"$SID"/SYS/exe/run
cp "$here/lib.sh" "$FS/lib.sh"
cat > "$FS/conf" <<CONF
SID=$SID
SIDADM=$SIDADM
HOST=$HOST
CT_RUN=$CT_RUN
INSTANCES="01:ASCS01:CS 00:D00:ABAP"
REMOTE="${REMOTE:-}"
SETTLE=${SETTLE:-8}
STARTSERVICE=${STARTSERVICE:-ok}
CONF
chmod 1777 "$FS/state"; : > "$FS/calls.log"; chmod 666 "$FS/calls.log"

kfile() { # dir name patch
  case " $LIBS " in *" $2 "*) printf 'fake SAP library %s 793 patch %s\n' "$2" "$3" > "$1/$2"; chmod 755 "$1/$2"; return ;; esac
  printf '#!/bin/sh\n# fake SAP kernel file %s, 793 patch %s\nKM_PATCH=%s\n. /usr/sap/.fakesap/lib.sh\n' "$2" "$3" "$3" > "$1/$2"
  chmod 755 "$1/$2"
}
for f in $EXES $LIBS SAPCAR; do kfile "$CT_RUN" "$f" 300; done
for i in ASCS01 D00; do # what sapcpe copied at the last start
  mkdir -p /usr/sap/"$SID"/$i/exe /usr/sap/"$SID"/$i/work
  for f in $EXES $LIBS; do kfile /usr/sap/"$SID"/$i/exe "$f" 300; done
done
for f in saphostctrl saphostexec sapcontrol; do kfile /usr/sap/hostctrl/exe $f 65; done

P=/sapmnt/$SID/profile
printf 'SAPSYSTEMNAME = %s\nSAPGLOBALHOST = %s\nDIR_CT_RUN = $(DIR_EXE_ROOT)$(DIR_SEP)$(OS_UNICODE)$(DIR_SEP)linuxx86_64\n' "$SID" "$HOST" > "$P/DEFAULT.PFL"
# the ASCS profile has no sapcpe: its exe directory only changes when KernelMan extracts into it
printf 'SAPSYSTEMNAME = %s\nSAPSYSTEM = 01\nINSTANCE_NAME = ASCS01\nDIR_EXECUTABLE = $(DIR_INSTANCE)/exe\nStart_Program_01 = local $(_MS) pf=$(_PF)\n' "$SID" > "$P/${SID}_ASCS01_$HOST"
printf 'SAPSYSTEMNAME = %s\nSAPSYSTEM = 00\nINSTANCE_NAME = D00\nDIR_EXECUTABLE = $(DIR_INSTANCE)/exe\nExecute_00 = immediate $(DIR_CT_RUN)/sapcpe$(FT_EXE) pf=$(_PF) $(_CPARG0)\nStart_Program_00 = local $(_DW) pf=$(_PF)\n' "$SID" > "$P/${SID}_D00_$HOST"
{ echo '#!/bin/sh'
  for i in ASCS01 D00; do
    e=/usr/sap/$SID/$i/exe
    echo "LD_LIBRARY_PATH=$e:\$LD_LIBRARY_PATH;export LD_LIBRARY_PATH;$e/sapstartsrv pf=/usr/sap/$SID/SYS/profile/${SID}_${i}_$HOST -D -u $SIDADM"
  done; } > /usr/sap/sapservices

B=$(mktemp -d) # archives are built outside /usr/sap so no scan finds the originals
mksar() { # name patch files...
  name=$1 patch=$2; shift 2
  d=$(mktemp -d); for f in "$@"; do kfile "$d" "$f" "$patch"; done
  echo "fake signature $name" > "$d/SIGNATURE.SMF"
  tar -cf "$B/$name" -C "$d" SIGNATURE.SMF "$@"; rm -rf "$d"
}
mksar SAPEXE_403-80007807.SAR 403 disp+work gwrd icman icmbnd.new sapuxuserchk msg_server enq_server enq_replicator sapstartsrv sapcontrol sapcpe saproot.sh libsapu16.so libicuuc.so.50
mksar SAPEXEDB_403-80007808.SAR 403 R3trans tp dbhdbslib.so
mksar dw_421-80007541.sar 421 disp+work
mksar SAPEXE_390-80007000.SAR 390 disp+work

dl=/home/$DLUSER/kernel
mkdir -p "$dl"; chmod 700 /home/"$DLUSER"
for f in SAPEXE_403-80007807.SAR SAPEXEDB_403-80007808.SAR dw_421-80007541.sar SAPEXE_390-80007000.SAR; do cp "$B/$f" "$dl/"; chmod 644 "$dl/$f"; done
touch -d '4 days ago' "$dl/SAPEXE_390-80007000.SAR"
chown -R "$DLUSER" "$dl"
# a backup directory holding a copy dated today: it must never be offered
mkdir -p /usr/sap/"$SID"/D00/exe_20261102 && cp "$B/SAPEXE_403-80007807.SAR" "$B"/sapstartsrv 2>/dev/null /usr/sap/"$SID"/D00/exe_20261102/ || true
kfile /usr/sap/"$SID"/D00/exe_20261102 sapstartsrv 290; kfile /usr/sap/"$SID"/D00/exe_20261102 sapcontrol 290
rm -rf "$B"

chown -R "$SIDADM":sapsys /sapmnt/"$SID" /usr/sap/"$SID"
for f in icmbnd sapuxuserchk; do chown root:sapsys "$CT_RUN/$f"; chmod 4750 "$CT_RUN/$f"; done # what saproot.sh left
for n in 00 01; do echo 1 > "$FS/state/up_$n"; echo RUNNING > "$FS/state/run_$n"; done
echo 1 > "$FS/state/hostagent"; chmod 666 "$FS"/state/*
echo "fake SAP host ready: $SID ($SIDADM) on $HOST, kernel 793 patch 300, archives in $dl"
