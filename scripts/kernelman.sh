#!/bin/sh
# kernelman launcher for Linux/AIX (and macOS for the demo): runs the matching prebuilt binary.
# Expected layout:  <dir>/kernelman.sh   <dir>/bin/kernelman-<os>-<arch>
# No runtime (Go, Python, Java) is required on the host.
dir=$(cd "$(dirname "$0")" && pwd)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  aix) goos=aix; goarch=ppc64 ;;
  darwin)
    goos=darwin
    case "$(uname -m)" in
      arm64)  goarch=arm64 ;;
      x86_64) goarch=amd64 ;;
      *) echo "kernelman: unsupported macOS architecture: $(uname -m)" >&2; exit 2 ;;
    esac ;;
  linux)
    goos=linux
    case "$(uname -m)" in
      x86_64)  goarch=amd64 ;;
      ppc64le) goarch=ppc64le ;;
      ppc64)   goarch=ppc64 ;;
      s390x)   goarch=s390x ;;
      *) echo "kernelman: unsupported Linux architecture: $(uname -m)" >&2; exit 2 ;;
    esac ;;
  *) echo "kernelman: unsupported operating system: $os" >&2; exit 2 ;;
esac
bin="$dir/bin/kernelman-$goos-$goarch"
if [ ! -x "$bin" ]; then
  echo "kernelman: binary not found or not executable: $bin" >&2
  exit 2
fi
exec "$bin" "$@"
