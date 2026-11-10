#!/usr/bin/env bash
# Run in a disposable Ubuntu 24.04 container. Only external package/service/Docker
# commands are simulated; exercise the complete installer CLI and real OS detection.
set -euo pipefail
repository=$(cd "$(dirname "$0")/.." && pwd)
export INSTALLER_PROBE
INSTALLER_PROBE=$(mktemp -d)
trap 'rm -rf -- "$INSTALLER_PROBE"' EXIT
mkdir "$INSTALLER_PROBE/bin"
export PATH="$INSTALLER_PROBE/bin:$PATH"
cat >"$INSTALLER_PROBE/docker.stub" <<'SH'
#!/bin/sh
shift 2 # explicit --context default
case "$1 $2" in
  'compose version') test -f "$INSTALLER_PROBE/compose-installed" ;;
  'info '|'ps -aq'|'volume ls') exit 0 ;;
  compose*) exit 0 ;;
  *) echo "Unexpected Docker operation: $*" >&2; exit 1 ;;
esac
SH
cat >"$INSTALLER_PROBE/bin/apt-get" <<'SH'
#!/bin/sh
set -eu
echo "$*" >>"$INSTALLER_PROBE/packages.log"
if [ "$1" = install ]; then
  if [ "${PLUGIN_ONLY:-}" = true ]; then
    case " $* " in *' docker.io '*) echo 'Attempted to replace an existing Engine' >&2; exit 1 ;; esac
    case " $* " in *' --no-install-recommends '*) ;; *) exit 1 ;; esac
    test "${NEEDRESTART_MODE:-}" = l
  fi
  cp "$INSTALLER_PROBE/docker.stub" "$INSTALLER_PROBE/bin/docker"
  chmod 755 "$INSTALLER_PROBE/bin/docker"
  touch "$INSTALLER_PROBE/compose-installed"
fi
SH
cat >"$INSTALLER_PROBE/bin/systemctl" <<'SH'
#!/bin/sh
echo "$*" >>"$INSTALLER_PROBE/services.log"
SH
cat >"$INSTALLER_PROBE/bin/dpkg-query" <<'SH'
#!/bin/sh
printf 'install ok installed'
SH
cat >"$INSTALLER_PROBE/bin/curl" <<'SH'
#!/bin/sh
for argument do last=$argument; done
case "$last" in */api/dashboard) printf 401 ;; */healthz) printf ok ;; *) exit 1 ;; esac
SH
chmod 755 "$INSTALLER_PROBE/bin/"*
! command -v docker
bash "$repository/install.sh" --source-dir "$repository" --domain studio.example.com --directory "$INSTALLER_PROBE/fresh" >"$INSTALLER_PROBE/fresh.log"
grep -q 'Ready at https://studio.example.com' "$INSTALLER_PROBE/fresh.log"
grep -q 'docker.io docker-compose-v2' "$INSTALLER_PROBE/packages.log"
grep -q 'enable --now docker' "$INSTALLER_PROBE/services.log"
# Existing distro Engine, missing plugin: only install Compose, never restart it.
rm "$INSTALLER_PROBE/compose-installed"
: >"$INSTALLER_PROBE/services.log"
PLUGIN_ONLY=true bash "$repository/install.sh" --source-dir "$repository" --domain studio.example.com --directory "$INSTALLER_PROBE/plugin" >"$INSTALLER_PROBE/plugin.log"
grep -q 'Ready at https://studio.example.com' "$INSTALLER_PROBE/plugin.log"
test ! -s "$INSTALLER_PROBE/services.log"
echo 'Ubuntu runtime bootstrap and plugin-only installation checks passed.'
