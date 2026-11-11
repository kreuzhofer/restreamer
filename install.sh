#!/usr/bin/env bash
# Keep execution at EOF so a truncated download cannot partially install.
set -euo pipefail

main() {
  local directory=/opt/restreamer domain= image= source_dir= ref=main prepare=false update=false stage=
  local username=admin password= username_set=false password_set=false
  local file existing=false label attempt code volumes containers cleanup compose_env= update_env=
  fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
  # Escape dotenv syntax and Compose interpolation, including trailing slashes.
  dotenv_value() {
    local value=$1
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    value=${value//\$/\$\$}
    printf '"%s"' "$value"
  }
  saved_credential_matches() {
    local name=$1 value=$2 saved
    saved=$(sed -n "/^$name=/p" "$directory/.env")
    [ "$saved" = "$name=$(dotenv_value "$value")" ] && return 0
    # Compatibility with the original installer’s unquoted generated values.
    [[ "$value" =~ ^[a-zA-Z0-9_.@-]+$ ]] && [ "$saved" = "$name=$value" ]
  }
  usage() {
    cat <<'USAGE'
Usage: sudo bash install.sh [options]
  --domain NAME        Public DNS name; prompted on first installation if omitted
  --username NAME      Initial dashboard username (default admin)
  --password VALUE     Initial dashboard password (default securely generated)
  --directory PATH     Installation directory (default /opt/restreamer)
  --image IMAGE        Published Restreamer image (default ghcr.io/kreuzhofer/restreamer:main)
  --ref REF            Repository ref for deployment files (default main)
  --source-dir PATH    Read deployment files from a local repository checkout
  --update             Pull configured images and restart an existing idle installation
  --prepare-only       Write private configuration without installing or starting services
  --help               Show this help
USAGE
  }
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --domain|--username|--password|--directory|--image|--source-dir|--ref)
        [ "$#" -ge 2 ] || fail "Missing value for $1"
        case "$1" in
          --domain) domain=$2 ;; --directory) directory=$2 ;; --image) image=$2 ;;
          --source-dir) source_dir=$2 ;; --ref) ref=$2 ;;
          --username) username=$2; username_set=true ;;
          --password) password=$2; password_set=true ;;
        esac
        shift 2 ;;
      --prepare-only) prepare=true; shift ;;
      --update) update=true; shift ;;
      --help) usage; return ;;
      *) fail "Unknown option: $1" ;;
    esac
  done
  if $update && $prepare; then fail '--update cannot be combined with --prepare-only.'; fi
  [ -z "$image" ] || [[ "$image" =~ ^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$ ]] || fail 'Invalid container image reference.'
  [[ "$username" =~ [^[:space:]] && "$username" != *:* && "$username" != *$'\n'* && "$username" != *$'\r'* ]] || fail 'Username must be nonempty and contain no colon or line break.'
  if $password_set; then
    [[ "$password" =~ [^[:space:]] && "$password" != *$'\n'* && "$password" != *$'\r'* ]] || fail 'Password must be nonempty and contain no line break.'
  fi
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
    $update || [ -z "$image" ] || [ "RESTREAMER_IMAGE=$image" = "$(sed -n '/^RESTREAMER_IMAGE=/p' "$directory/.env")" ] || fail 'Image differs from the saved installation; use the documented update procedure.'
    domain=$(cat "$directory/.domain")
    if $username_set && ! saved_credential_matches DASHBOARD_USERNAME "$username"; then
      fail 'Credentials differ from the saved installation; rerun without credential flags. Nothing was changed.'
    fi
    if $password_set && ! saved_credential_matches DASHBOARD_PASSWORD "$password"; then
      fail 'Credentials differ from the saved installation; rerun without credential flags. Nothing was changed.'
    fi
    existing=true
  fi
  if $update && ! $existing; then fail '--update requires an existing managed installation.'; fi
  if $update && [ ! -f "$directory/.deployment-initialized" ]; then fail 'Run the installer normally once before using --update.'; fi
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
    if ! $password_set; then
      password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
      [ "${#password}" -eq 48 ] || fail 'Could not generate dashboard credentials.'
    fi
    cat >"$stage/.env" <<ENV
COMPOSE_PROJECT_NAME=restreamer-standalone
COMPOSE_FILE=docker-compose.yml:compose.https.yml
RESTREAMER_DOMAIN=$domain
RESTREAMER_IMAGE=$image
DASHBOARD_USERNAME=$(dotenv_value "$username")
DASHBOARD_PASSWORD=$(dotenv_value "$password")
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
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 ca-certificates curl python3
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
  compose_env="$directory/.env"
  compose() {
    docker_cli compose --project-name restreamer-standalone --project-directory "$directory" \
      --env-file "$compose_env" -f "$directory/docker-compose.yml" -f "$directory/compose.https.yml" "$@"
  }
  if [ ! -f "$directory/.deployment-initialized" ]; then
    containers=$(docker_cli ps -aq --filter label=com.docker.compose.project=restreamer-standalone) || fail 'Could not inspect existing Docker containers; refusing to initialize.'
    [ -z "$containers" ] || fail 'A standalone installation already exists in Docker; use its original installation directory.'
    volumes=$(docker_cli volume ls --format '{{.Name}}') || fail 'Could not inspect existing Docker volumes; refusing to initialize.'
    for label in restreamer-state restreamer-events caddy-data caddy-config; do
      if printf '%s\n' "$volumes" | grep -Fxq "restreamer-standalone_$label"; then
        fail 'Saved standalone data already exists in Docker; use its original installation directory. No data was changed.'
      fi
    done
    touch "$directory/.deployment-initialized"
  fi
  compose config --quiet
  update_idle() {
    local container
    container=$(compose ps --all --quiet restreamer) || fail 'Could not inspect the app; update refused.'
    [ -n "$container" ] || return 0
    command -v python3 >/dev/null || fail 'Install Python 3 to verify the studio is idle before updating.'
    python3 -I - "$container" <<'IDLE_CHECK'
import base64, ipaddress, json, subprocess, sys, urllib.request
try:
    data = json.loads(subprocess.check_output(
        ['docker', '--context', 'default', 'inspect', sys.argv[1]], stderr=subprocess.DEVNULL))[0]
    if not data['State']['Running']:
        sys.exit(0)
    values = dict(item.split('=', 1) for item in data['Config']['Env'] if '=' in item)
    user, password = values.get('DASHBOARD_USERNAME', ''), values.get('DASHBOARD_PASSWORD', '')
    if not user or not password:
        raise ValueError('authentication unavailable')
    address = None
    for binding in data['NetworkSettings']['Ports'].get('8080/tcp') or []:
        host = {'0.0.0.0': '127.0.0.1', '::': '::1'}.get(binding['HostIp'], binding['HostIp'])
        if ipaddress.ip_address(host).is_loopback:
            host = '[' + host + ']' if ':' in host else host
            address = 'http://' + host + ':' + binding['HostPort']
            break
    if address is None:
        raise ValueError('local dashboard binding unavailable')
    auth = base64.b64encode((user + ':' + password).encode()).decode()
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    def get(path):
        request = urllib.request.Request(address + path, headers={'Authorization': 'Basic ' + auth})
        with opener.open(request, timeout=5) as response:
            return json.load(response)
    stage, ingestion = get('/api/stage'), get('/api/ingestion')
    if stage.get('stage') != 'OFF' or stage.get('pending') is not None or ingestion.get('publisher_connected') is not False:
        sys.exit('Error: Stop the broadcast/rehearsal and disconnect OBS before --update. Nothing was restarted.')
except Exception:
    sys.exit('Error: Cannot verify that the studio is idle; update refused. Check app health and its local dashboard binding, or deliberately stop the app before retrying.')
IDLE_CHECK
  }
  if $update; then
    update_idle
    if [ -n "$image" ]; then
      umask 077
      update_env=$(mktemp "$directory/.env.update.XXXXXX")
      printf -v cleanup 'rm -f -- %q' "$update_env"
      trap "$cleanup" EXIT
      sed '/^RESTREAMER_IMAGE=/d' "$directory/.env" >"$update_env"
      printf 'RESTREAMER_IMAGE=%s\n' "$image" >>"$update_env"
      compose_env=$update_env
      compose config --quiet
    fi
    printf 'Pulling configured images; saved data and configuration are retained…\n'
    compose pull --policy always
    update_idle
    if [ -n "$update_env" ]; then
      cp -p "$directory/.env" "$directory/.env.before-update.$(date +%Y%m%dT%H%M%S).$$"
      mv "$update_env" "$directory/.env"
      compose_env="$directory/.env"
      trap - EXIT
    fi
    printf 'Restarting the idle installation with the pulled images…\n'
    compose up -d --force-recreate --pull never --wait --wait-timeout 180
  else
    printf 'Starting Restreamer and HTTPS proxy; existing containers will not be recreated…\n'
    compose up -d --no-recreate --pull missing --wait --wait-timeout 120
  fi
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
