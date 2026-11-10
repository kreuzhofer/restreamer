#!/usr/bin/env bash
# Keep execution at EOF so a truncated download cannot partially install.
set -euo pipefail

main() {
  local directory=/opt/restreamer domain= image= source_dir= ref=main prepare=false stage=
  local file password existing=false label attempt code volumes containers cleanup
  fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
  usage() {
    cat <<'USAGE'
Usage: sudo bash install.sh [options]
  --domain NAME        Public DNS name; prompted on first installation if omitted
  --directory PATH     Installation directory (default /opt/restreamer)
  --image IMAGE        Published Restreamer image (default ghcr.io/kreuzhofer/restreamer:main)
  --ref REF            Repository ref for deployment files (default main)
  --source-dir PATH    Read deployment files from a local repository checkout
  --prepare-only       Write private configuration without installing or starting services
  --help               Show this help
USAGE
  }
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --domain|--directory|--image|--source-dir|--ref)
        [ "$#" -ge 2 ] || fail "Missing value for $1"
        case "$1" in
          --domain) domain=$2 ;; --directory) directory=$2 ;; --image) image=$2 ;;
          --source-dir) source_dir=$2 ;; --ref) ref=$2 ;;
        esac
        shift 2 ;;
      --prepare-only) prepare=true; shift ;;
      --help) usage; return ;;
      *) fail "Unknown option: $1" ;;
    esac
  done
  domain=$(printf '%s' "$domain" | tr '[:upper:]' '[:lower:]')
  if ! $prepare; then
    [ "$(uname -s)" = Linux ] || fail 'Service installation requires Linux.'
    [ "$(id -u)" = 0 ] || fail 'Run the installer with sudo or as root.'
  fi
  [[ "$directory" = /* && "$directory" != / && "$directory" != */ && "$directory" != *$'\n'* ]] || fail 'Use an absolute installation directory, without a trailing slash.'
  [[ "$ref" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/-]*$ ]] || fail 'Invalid repository ref.'
  [[ "$ref" != *..* ]] || fail 'Invalid repository ref.'
  [ ! -L "$directory" ] || fail 'The installation directory must not be a symlink.'
  if [ -e "$directory" ]; then
    [ -f "$directory/.restreamer-installer" ] || fail 'Directory already exists and is not managed by this installer; nothing was changed.'
    [ "$(cat "$directory/.restreamer-installer")" = v1 ] || fail 'Unknown installation format.'
    for file in .env .domain docker-compose.yml compose.https.yml Caddyfile; do
      [ -f "$directory/$file" ] && [ ! -L "$directory/$file" ] || fail "Missing or unsafe installation file: $file"
    done
    [ -z "$domain" ] || [ "$domain" = "$(cat "$directory/.domain")" ] || fail 'Domain differs from the saved installation; refusing to overwrite it.'
    [ -z "$image" ] || [ "RESTREAMER_IMAGE=$image" = "$(sed -n '/^RESTREAMER_IMAGE=/p' "$directory/.env")" ] || fail 'Image differs from the saved installation; use the documented update procedure.'
    domain=$(cat "$directory/.domain")
    existing=true
  fi
  if [ -z "$domain" ]; then
    read -r -p 'Dashboard domain (for example studio.example.com): ' domain </dev/tty || fail 'No terminal available; supply --domain.'
  fi
  domain=$(printf '%s' "$domain" | tr '[:upper:]' '[:lower:]')
  # A single public hostname only: never interpolate Caddyfile syntax or a URL.
  [ "${#domain}" -le 253 ] && [[ "$domain" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$ ]] || fail 'Enter a DNS hostname, without a scheme, path, port, wildcard or spaces.'
  [[ ! "$domain" =~ ^[0-9.]+$ ]] || fail 'Use a domain name, not an IP address.'
  case "$domain" in *.local|*.localhost|*.internal|*.home.arpa|*.ts.net) fail 'Use a public domain supported by Let’s Encrypt.' ;; esac
  if ! $existing; then
    image=${image:-ghcr.io/kreuzhofer/restreamer:main}
    [[ "$image" =~ ^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$ ]] || fail 'Invalid container image reference.'
    umask 077
    mkdir -p "$(dirname "$directory")"
    stage=$(mktemp -d "${directory}.prepare.XXXXXX")
    # Capture the path now: newer Bash unwinds function locals before EXIT traps.
    printf -v cleanup 'rm -rf -- %q' "$stage"
    trap "$cleanup" EXIT
    for file in docker-compose.yml deploy/compose.https.yml deploy/Caddyfile; do
      if [ -n "$source_dir" ]; then
        cp "$source_dir/$file" "$stage/$(basename "$file")"
      else
        curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
          --connect-timeout 10 --max-time 60 \
          "https://raw.githubusercontent.com/kreuzhofer/restreamer/$ref/$file" --output "$stage/$(basename "$file")"
      fi
    done
    password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
    [ "${#password}" -eq 48 ] || fail 'Could not generate dashboard credentials.'
    cat >"$stage/.env" <<ENV
COMPOSE_PROJECT_NAME=restreamer-standalone
COMPOSE_FILE=docker-compose.yml:compose.https.yml
RESTREAMER_DOMAIN=$domain
RESTREAMER_IMAGE=$image
DASHBOARD_USERNAME=admin
DASHBOARD_PASSWORD=$password
STATUS_BIND_ADDRESS=127.0.0.1
STATUS_PORT=8080
RTMP_BIND_ADDRESS=0.0.0.0
INGESTION_SERVER_URL=rtmp://$domain:1935/live
ENV
    printf '%s\n' "$domain" >"$stage/.domain"
    printf 'v1\n' >"$stage/.restreamer-installer"
    mkdir -m 700 "$directory"
    cp -R "$stage/." "$directory/"
    rm -rf -- "$stage"
    stage=
    trap - EXIT
  fi
  if $prepare; then
    printf 'Prepared %s. No services started. Credentials are in %s/.env.\n' "$directory" "$directory"
    return
  fi
  [ "$(stat -c %u "$directory")" = 0 ] || fail 'Installation directory must be owned by root. Prepare it with sudo.'
  directory=$(cd "$directory" && pwd -P)
  exec 9>"$directory/.install.lock"
  flock -n 9 || fail 'Another installer is using this directory.'
  # Never use a remote Docker context or let caller environment override saved credentials.
  unset DASHBOARD_USERNAME DASHBOARD_PASSWORD RESTREAMER_DOMAIN RESTREAMER_IMAGE
  unset STATUS_BIND_ADDRESS STATUS_PORT RTMP_BIND_ADDRESS BRB_ENABLED INGEST_STREAM_KEY INGESTION_SERVER_URL
  unset COMPOSE_FILE COMPOSE_PROJECT_NAME
  docker_cli() { docker --context default "$@"; }
  if ! command -v docker >/dev/null 2>&1; then
    [ -r /etc/os-release ] || fail 'Install Docker Engine and its Compose plugin first.'
    # This root-owned operating-system file is the distro-defined interface.
    . /etc/os-release
    [ "${ID:-}" = ubuntu ] && [ "${VERSION_ID:-}" = 24.04 ] || fail 'Automatic Docker setup supports Ubuntu 24.04. On other Linux systems, install Docker Engine and Compose first.'
    printf 'Installing Docker and Compose from the Ubuntu repositories…\n'
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 ca-certificates curl
    systemctl enable --now docker
  elif ! docker_cli compose version >/dev/null 2>&1; then
    # Add only the plugin to an Ubuntu-packaged Engine. Never replace Docker CE
    # or restart a running daemon just to install Compose.
    [ -r /etc/os-release ] || fail 'Install the Compose v2 plugin for your existing Docker installation.'
    . /etc/os-release
    [ "${ID:-}" = ubuntu ] && [ "${VERSION_ID:-}" = 24.04 ] && \
      [ "$(dpkg-query -W -f='${Status}' docker.io 2>/dev/null)" = 'install ok installed' ] || fail 'Install the Compose v2 plugin for your existing Docker installation; Docker packages were not replaced.'
    apt-get update
    DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y --no-install-recommends docker-compose-v2
  fi
  docker_cli compose version >/dev/null || fail 'Install the Docker Compose v2 plugin; existing Docker packages were not changed.'
  docker_cli info >/dev/null || fail 'The local Docker daemon is unavailable. Start Docker, then rerun this command.'
  command -v curl >/dev/null || fail 'Install curl, then rerun this command.'
  compose() {
    docker_cli compose --project-name restreamer-standalone --project-directory "$directory" \
      --env-file "$directory/.env" -f "$directory/docker-compose.yml" -f "$directory/compose.https.yml" "$@"
  }
  if [ ! -f "$directory/.deployment-initialized" ]; then
    containers=$(docker_cli ps -aq --filter label=com.docker.compose.project=restreamer-standalone) || fail 'Could not inspect existing Docker containers; refusing to initialize.'
    [ -z "$containers" ] || fail 'A standalone installation already exists in Docker; use its original installation directory.'
    volumes=$(docker_cli volume ls --format '{{.Name}}') || fail 'Could not inspect existing Docker volumes; refusing to initialize.'
    for label in restreamer-state caddy-data caddy-config; do
      if printf '%s\n' "$volumes" | grep -Fxq "restreamer-standalone_$label"; then
        fail 'Saved standalone data already exists in Docker; use its original installation directory. No data was changed.'
      fi
    done
    touch "$directory/.deployment-initialized"
  fi
  compose config --quiet
  printf 'Starting Restreamer and HTTPS proxy; existing containers will not be recreated…\n'
  compose up -d --no-recreate --pull missing --wait --wait-timeout 120
  printf 'Waiting for a trusted certificate and healthy HTTPS endpoint for %s…\n' "$domain"
  for attempt in {1..60}; do
    if curl --fail --silent --show-error --noproxy "$domain" --connect-timeout 2 --max-time 3 \
        --resolve "$domain:443:127.0.0.1" "https://$domain/healthz" >/dev/null 2>&1; then
      code=$(curl --silent --noproxy "$domain" --connect-timeout 2 --max-time 3 \
        --resolve "$domain:443:127.0.0.1" --output /dev/null --write-out '%{http_code}' "https://$domain/api/dashboard") || code=000
      [ "$code" = 401 ] || fail 'HTTPS responded but dashboard authentication was not enforced. Inspect the saved configuration.'
      printf 'Ready at https://%s\nLogin credentials are stored in %s/.env (root only).\n' "$domain" "$directory"
      printf 'View credentials: sudo cat %s/.env\n' "$directory"
      return
    fi
    sleep 2
  done
  fail "HTTPS is not ready. Check A/AAAA DNS records, inbound TCP 80/443 and certificate logs: cd $directory && sudo docker compose -p restreamer-standalone -f docker-compose.yml -f compose.https.yml logs caddy. Configuration and data were preserved; fix the cause and rerun."
}

main "$@"
