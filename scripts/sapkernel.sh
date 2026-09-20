#!/bin/sh
# sapkernel launcher for Unix/AIX: runs the prebuilt binary that matches this host.
# Expected layout:  <dir>/sapkernel.sh   <dir>/bin/sapkernel-<os>-<arch>
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
      *) echo "sapkernel: unsupported Linux architecture: $(uname -m)" >&2; exit 2 ;;
    esac ;;
  *) echo "sapkernel: unsupported operating system: $os" >&2; exit 2 ;;
esac
bin="$dir/bin/sapkernel-$goos-$goarch"
if [ ! -x "$bin" ]; then
  echo "sapkernel: binary not found or not executable: $bin" >&2
  exit 2
fi
exec "$bin" "$@"
