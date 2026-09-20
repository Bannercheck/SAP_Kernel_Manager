#!/bin/sh
# skm launcher for Unix/AIX: runs the prebuilt binary that matches this host.
# Expected layout:  <dir>/skm.sh   <dir>/bin/skm-<os>-<arch>
# No runtime (Go, Python, Java) is required on the host.
dir=$(cd "$(dirname "$0")" && pwd)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  aix) goos=aix; goarch=ppc64 ;;
  linux)
    goos=linux
    case "$(uname -m)" in
      x86_64)  goarch=amd64 ;;
      ppc64le) goarch=ppc64le ;;
      ppc64)   goarch=ppc64 ;;
      s390x)   goarch=s390x ;;
      *) echo "skm: unsupported Linux architecture: $(uname -m)" >&2; exit 2 ;;
    esac ;;
  *) echo "skm: unsupported operating system: $os" >&2; exit 2 ;;
esac
bin="$dir/bin/skm-$goos-$goarch"
if [ ! -x "$bin" ]; then
  echo "skm: binary not found or not executable: $bin" >&2
  exit 2
fi
exec "$bin" "$@"
