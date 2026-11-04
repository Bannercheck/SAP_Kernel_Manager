# KernelMan fake SAP host — the behaviour behind every fake SAP executable.
# Each fake kernel file is a tiny script that sets KM_PATCH and sources this
# file; dispatch is on the program name. State lives in $FS/state so root,
# <sid>adm and sudo users all see the same system. Test tool only.
FS=/usr/sap/.fakesap
. "$FS/conf"
prog=$(basename "$0")
now=$(date +%s)
me=$(id -un)
echo "$(date '+%H:%M:%S') $me $prog $*" >> "$FS/calls.log" 2>/dev/null

hdr() { printf '\n%s\n%s\n' "$(date '+%d.%m.%Y %H:%M:%S')" "$1"; }
ok() { hdr "$1"; echo OK; }
fail() { hdr "$1"; echo "FAIL: $2"; exit "${3:-1}"; }
st() { cat "$FS/state/$1" 2>/dev/null; }
put() { echo "$2" > "$FS/state/$1"; }
patch_of() { sed -n 's/^KM_PATCH=//p' "$1" 2>/dev/null | head -1; }
inst_of() { for i in $INSTANCES; do [ "${i%%:*}" = "$1" ] && echo "$i" && return 0; done; return 1; }
exe_of() { echo "/usr/sap/$SID/$1/exe"; }
pf_of() { echo "/usr/sap/$SID/SYS/profile/${SID}_$1_$HOST"; }
admin() { [ "$me" = root ] || [ "$me" = "$SIDADM" ]; }

# run state: RUNNING | STOPPED | STOPPING:<t> | STARTING:<t>, settles after $SETTLE seconds
runstate() {
  s=$(st "run_$1"); t=${s#*:}
  case "$s" in
    STOPPING:*) [ $((now - t)) -ge "$SETTLE" ] && { put "run_$1" STOPPED; s=STOPPED; } ;;
    STARTING:*) [ $((now - t)) -ge "$SETTLE" ] && { put "run_$1" RUNNING; s=RUNNING; } ;;
  esac
  echo "${s%%:*}"
}

sapcpe() { # copy the central kernel into the instance directory, like sapcpe at start
  for f in "$CT_RUN"/*; do [ -f "$f" ] && cp -p "$f" "$(exe_of "$1")/" 2>/dev/null; done
}

start_inst() { # $1 = nr
  name=$(inst_of "$1" | cut -d: -f2)
  grep -qi sapcpe "$(pf_of "$name")" 2>/dev/null && sapcpe "$name"
  # an instance directory at another level than the central kernel does not come up
  if [ "$(patch_of "$(exe_of "$name")/disp+work")" != "$(patch_of "$CT_RUN/disp+work")" ] && [ -f "$(exe_of "$name")/disp+work" ]; then
    put "run_$1" "FAILED"; return
  fi
  put "run_$1" "STARTING:$now"
}

do_sapcontrol() {
  nr="" fn="" host=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -nr) nr=$2; shift 2 ;;
      -host) host=$2; shift 2 ;;
      -function) fn=$2; shift 2; break ;;
      *) shift ;;
    esac
  done
  [ -n "$host" ] && [ "$host" != "$HOST" ] && [ "$host" != localhost ] && fail "$fn" "NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()"
  inst=$(inst_of "$nr") || fail "$fn" "NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()"
  name=$(echo "$inst" | cut -d: -f2); typ=$(echo "$inst" | cut -d: -f3)
  case "$fn" in
    StartService)
      admin || fail "$fn" "Permission denied"
      [ "$STARTSERVICE" = fail ] && fail "$fn" "Instance service could not be started: sapstartsrv not registered (sapservices)"
      put "up_$nr" 1; ok "$fn"; exit 0 ;;
  esac
  [ "$(st "up_$nr")" = 1 ] || fail "$fn" "NIECONN_REFUSED (Connection refused), NiRawConnect failed in plugin_fopen()"
  case "$fn" in
    GetProcessList)
      rs=$(runstate "$nr")
      ok "$fn"; echo "name, description, dispstatus, textstatus, starttime, elapsedtime, pid"
      if [ "$typ" = CS ]; then procs="msg_server:MessageServer enq_server:Enqueue_Server_2"; else procs="disp+work:Dispatcher igswd_mt:IGS_Watchdog gwrd:Gateway icman:ICM"; fi
      code=0; first=1
      for p in $procs; do
        n=${p%%:*}; d=$(echo "${p#*:}" | tr _ ' ')
        case "$rs" in
          RUNNING) s="GREEN, Running"; code=3 ;;
          STOPPED) s="GRAY, Stopped"; code=4 ;;
          FAILED) s="GRAY, Stopped"; [ $first = 1 ] && s="RED, Dispatcher stopped: kernel level mismatch"; code=0 ;;
          STOPPING) s="GRAY, Stopped"; [ $first = 1 ] && s="YELLOW, Stopping"; code=0 ;;
          STARTING) s="GREEN, Running"; [ $first = 1 ] && s="YELLOW, Initializing"; code=0 ;;
        esac
        first=0
        echo "$n, $d, $s, 2026 11 04 08:00:00, 1:00:00, 4711"
      done
      exit $code ;;
    GetInstanceProperties)
      ok "$fn"; echo "property, propertytype, value"; echo "SAPSYSTEM, Attribute, $nr"; echo "SAPSYSTEMNAME, Attribute, $SID"
      echo "SAPLOCALHOST, Attribute, $HOST"; echo "INSTANCE_NAME, Attribute, $name"; exit 0 ;;
    ParameterValue)
      case "$1" in
        DIR_CT_RUN) v=$CT_RUN ;; DIR_EXE_ROOT) v=/usr/sap/$SID/SYS/exe ;; DIR_EXECUTABLE) v=$(exe_of "$name") ;;
        SAPPROFILE) v=$(pf_of "$name") ;; DIR_PROFILE) v=/usr/sap/$SID/SYS/profile ;; SAPSYSTEMNAME) v=$SID ;;
        INSTANCE_NAME) v=$name ;; SAPLOCALHOST) v=$HOST ;; SAPSYSTEM) v=$nr ;; dbms/type) v=hdb ;; SAPDBHOST) v=${HOST}db ;; dbs/hdb/dbname) v=H$SID ;;
        *) v="" ;;
      esac
      ok "$fn"; echo "$v"; exit 0 ;;
    GetSystemInstanceList)
      ok "$fn"; echo "hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus"
      for i in $INSTANCES; do
        n=${i%%:*}; t=$(echo "$i" | cut -d: -f3); s=GRAY
        [ "$(st "up_$n")" = 1 ] && case $(runstate "$n") in RUNNING) s=GREEN ;; STARTING|STOPPING) s=YELLOW ;; FAILED) s=RED ;; esac
        if [ "$t" = CS ]; then f="MESSAGESERVER|ENQUE"; p=1; else f="ABAP|GATEWAY|ICMAN|IGS"; p=3; fi
        echo "$HOST, ${n#0}, 5${n}13, 5${n}14, $p, $f, $s"
      done
      for r in $REMOTE; do echo "${r%%:*}, $(echo "$r" | cut -d: -f2 | sed 's/^0//'), 5$(echo "$r" | cut -d: -f2)13, 5$(echo "$r" | cut -d: -f2)14, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN"; done
      exit 0 ;;
    GetVersionInfo)
      ok "$fn"; echo "Filename, VersionInfo, Time, RKS Compatibility level"
      for f in sapstartsrv disp+work gwrd; do
        echo "$(exe_of "$name")/$f, 793, patch $(patch_of "$(exe_of "$name")/$f"), changelist 2123456, RKS compatibility level 0, optU (Oct 14 2026, 21:33:27), linuxx86_64, 2026 10 14 21:33:27, 0"
      done
      exit 0 ;;
    StopSystem|StartSystem|Start|Stop|StopService|RestartService|WaitforStopped|WaitforStarted)
      admin || fail "$fn" "Permission denied" ;;
    *) fail "$fn" "Invalid function" ;;
  esac
  case "$fn" in
    StopSystem) for i in $INSTANCES; do n=${i%%:*}; [ "$(st "up_$n")" = 1 ] && [ "$(runstate "$n")" != STOPPED ] && put "run_$n" "STOPPING:$now"; done ;;
    StartSystem) for i in $INSTANCES; do n=${i%%:*}; [ "$(st "up_$n")" = 1 ] && [ "$(runstate "$n")" != RUNNING ] && start_inst "$n"; done ;;
    Start) [ "$(runstate "$nr")" != RUNNING ] && start_inst "$nr" ;;
    Stop) [ "$(runstate "$nr")" != STOPPED ] && put "run_$nr" "STOPPING:$now" ;;
    StopService) put "up_$nr" 0 ;;
    RestartService) : ;;
    WaitforStopped|WaitforStarted)
      want=STOPPED; [ "$fn" = WaitforStarted ] && want=RUNNING
      end=$((now + ${1:-300}))
      while [ "$(runstate "$nr")" != $want ]; do
        [ "$(date +%s)" -ge $end ] && fail "$fn" "Timeout" 2
        sleep "${2:-2}"; now=$(date +%s)
      done ;;
  esac
  ok "$fn"; exit 0
}

do_sapcar() {
  mode="" ar="" dir=.
  while [ $# -gt 0 ]; do
    case "$1" in -xvf|-xf|-tvf) mode=$1; ar=$2; shift 2 ;; -R) dir=$2; shift 2 ;; *) shift ;; esac
  done
  [ -z "$mode" ] && { echo "usage: SAPCAR -xvf <archive> [-R <dir>]"; exit 1; }
  [ -r "$ar" ] || { echo "SAPCAR: could not open $ar (error 2). No such file or directory" >&2; exit 1; }
  tar -tf "$ar" >/dev/null 2>&1 || { echo "SAPCAR: $ar is not a SAR archive (error 14)" >&2; exit 1; }
  echo "SAPCAR: processing archive $ar (version 2.01)"
  [ "$mode" = -tvf ] && { tar -tf "$ar" | sed 's/^/-rwxr-xr-x  /'; exit 0; }
  tmp=$(mktemp -d) || exit 1
  tar -xf "$ar" -C "$tmp" --no-same-owner || { rm -rf "$tmp"; exit 1; }
  n=0
  for f in $(tar -tf "$ar"); do
    t="$dir/$f"
    # a running executable cannot be opened for writing (ETXTBSY): sapstartsrv of an instance whose service is up
    case "$f" in sapstartsrv)
      for i in $INSTANCES; do
        nm=$(echo "$i" | cut -d: -f2)
        if [ "$(cd "$dir" && pwd -P)" = "$(cd "$(exe_of "$nm")" 2>/dev/null && pwd -P)" ] && [ "$(st "up_${i%%:*}")" = 1 ]; then
          echo "SAPCAR: could not open for writing $t (error 26). Text file busy" >&2; rm -rf "$tmp"; exit 1
        fi
      done ;;
    esac
    echo "x $f"
    if [ -e "$t" ]; then cat "$tmp/$f" > "$t" || { echo "SAPCAR: could not open for writing $t (error 13). Permission denied" >&2; rm -rf "$tmp"; exit 1; }
    else cp "$tmp/$f" "$t" || { rm -rf "$tmp"; exit 1; }; fi
    n=$((n + 1))
  done
  rm -rf "$tmp"
  echo "SAPCAR: $n file(s) extracted"
  exit 0
}

case "$prog" in
  sapcontrol) do_sapcontrol "$@" ;;
  SAPCAR|sapcar) do_sapcar "$@" ;;
  disp+work)
    [ "$1" = -V ] || exit 0
    printf -- '--------------------\ndisp+work information\n--------------------\n\nkernel release                793\n\nkernel make variant           793_REL\n\n'
    printf 'DBMS client library           SQLDBC 2.20.14 (fake)\n\ncompiled on                   Linux GNU SLES-12 x86_64 cc9.3.1 for linuxx86_64\n\n'
    printf 'compiled for                  64 BIT\n\ncompilation mode              UNICODE\n\ncompile time                  Oct 14 2026 21:33:27\n\npatch number                  %s\n\n' "$KM_PATCH"
    exit 0 ;;
  saphostctrl)
    for i in $INSTANCES; do echo " Inst Info : $SID - ${i%%:*} - $HOST - 793, patch $(patch_of "$CT_RUN/disp+work"), changelist 2123456"; done
    exit 0 ;;
  saphostexec)
    case "$1" in
      -version) printf -- '--------------------\nsaphostexec information\n--------------------\n\nkernel release                722\n\ncompiled on                   Linux GNU SLES-12 x86_64 for linuxx86_64\n\npatch number                  %s\n' "$KM_PATCH" ;;
      -status) if [ "$(st hostagent)" = 1 ]; then echo "saphostexec running (pid = 4242)"; echo "sapstartsrv running (pid = 4243)"; echo "saposcol running (pid = 4244)"; else echo "saphostexec stopped"; fi ;;
      -restart|-start) [ "$me" = root ] || { echo "saphostexec: must be root" >&2; exit 1; }; put hostagent 1; echo "start hostcontrol using profile /usr/sap/hostctrl/exe/host_profile" ;;
    esac
    exit 0 ;;
  sapstartsrv)
    pf=""; for a in "$@"; do case "$a" in pf=*) pf=${a#pf=} ;; esac; done
    inst=$(basename "$pf" | cut -d_ -f2); nr=$(echo "$inst" | sed 's/.*\([0-9][0-9]\)$/\1/')
    [ -r "$pf" ] || { echo "sapstartsrv: profile $pf not readable" >&2; exit 1; }
    put "up_$nr" 1; exit 0 ;;
  sapcpe) pf=""; for a in "$@"; do case "$a" in pf=*) pf=${a#pf=} ;; esac; done; sapcpe "$(basename "$pf" | cut -d_ -f2)"; exit 0 ;;
  saproot.sh)
    [ "$me" = root ] || { echo "saproot.sh: must be run as root" >&2; exit 1; }
    cd "$CT_RUN" || exit 1
    [ -f icmbnd.new ] && mv -f icmbnd.new icmbnd
    for f in icmbnd sapuxuserchk; do [ -f "$f" ] && chown root:sapsys "$f" && chmod 4750 "$f"; done
    echo "saproot.sh: $SID done"; exit 0 ;;
esac
exit 0
