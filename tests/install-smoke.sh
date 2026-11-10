#!/usr/bin/env bash
# Real Docker/Caddy/app boundary, using an isolated local CA (no public ACME requests).
set -euo pipefail
repository=$(cd "$(dirname "$0")/.." && pwd)
# This test uses the installer's fixed project name. Refuse occupied hosts before
# creating an installer marker or installing a teardown trap. Failed queries must
# also stop: absence of Docker output is not proof that the project is unused.
containers=$(sudo docker --context default ps -aq --filter label=com.docker.compose.project=restreamer-standalone)
volumes=$(sudo docker --context default volume ls -q --filter label=com.docker.compose.project=restreamer-standalone)
all_volumes=$(sudo docker --context default volume ls --format '{{.Name}}')
reserved_volumes=$(printf '%s\n' "$all_volumes" | grep -Ex 'restreamer-standalone_(restreamer-state|caddy-data|caddy-config)' || true)
if [ -n "$containers" ] || [ -n "$volumes" ] || [ -n "$reserved_volumes" ]; then
  echo 'Refusing smoke test: restreamer-standalone already has containers or volumes; use a disposable Docker host.' >&2
  exit 1
fi
test_root=$(mktemp -d)
install_directory=$test_root/app
domain=studio.example.test
compose() { sudo docker --context default compose --project-directory "$install_directory" --env-file "$install_directory/.env" -p restreamer-standalone -f "$install_directory/docker-compose.yml" -f "$install_directory/compose.https.yml" "$@"; }
cleanup() {
  if sudo test -f "$install_directory/.restreamer-installer"; then
    compose logs --tail 30 caddy || true
    compose down --volumes || true
  fi
  sudo rm -rf -- "$test_root"
}
trap cleanup EXIT
sudo bash "$repository/install.sh" --prepare-only --domain "$domain" --directory "$install_directory" \
  --source-dir "$repository" --image restreamer:test
sudo docker run --rm --network none -e RESTREAMER_DOMAIN="$domain" \
  -v "$install_directory/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2 \
  caddy adapt --config /etc/caddy/Caddyfile --validate >/dev/null
# Only this disposable test fixture replaces the public issuer with a local CA.
printf '%s {\n tls internal\n reverse_proxy restreamer:8080\n}\n' "$domain" | sudo tee "$install_directory/Caddyfile" >/dev/null
sudo env CURL_CA_BUNDLE="$test_root/ca.pem" bash "$repository/install.sh" --directory "$install_directory" >"$test_root/installer.log" 2>&1 &
installer_pid=$!
for attempt in {1..60}; do
  container=$(compose ps -q caddy 2>/dev/null || true)
  if [ -n "$container" ] && sudo docker cp "$container:/data/caddy/pki/authorities/local/root.crt" "$test_root/ca.pem" 2>/dev/null; then
    sudo chmod 644 "$test_root/ca.pem"
    break
  fi
  sleep 2
done
if ! wait "$installer_pid"; then cat "$test_root/installer.log"; exit 1; fi
grep -q "Ready at https://$domain" "$test_root/installer.log"
password=$(sudo sed -n 's/^DASHBOARD_PASSWORD=//p' "$install_directory/.env")
api() { curl --fail --silent --show-error --noproxy "$domain" --cacert "$test_root/ca.pem" --resolve "$domain:443:127.0.0.1" --user "admin:$password" "$@"; }
test "$(curl --silent --noproxy "$domain" --cacert "$test_root/ca.pem" --resolve "$domain:443:127.0.0.1" -o /dev/null -w '%{http_code}' "https://$domain/api/dashboard")" = 401
test "$(curl --silent --noproxy "$domain" --resolve "$domain:80:127.0.0.1" -o /dev/null -w '%{http_code}' "http://$domain/")" = 308
api "https://$domain/live" >/dev/null
api "https://$domain/api/ingestion" | jq -e --arg address "rtmp://$domain:1935/live" ' .server_url == $address and (has("key") | not)'
api "https://$domain/api/stage" | jq -e '.stage == "OFF"'
# A healthy login page alone does not prove the installed studio is usable.
api "https://$domain/api/generator/templates" | jq -e 'any(.[]; .id == "starter-prestream" and .builtin) and any(.[]; .id == "starter-ending" and .builtin) and any(.[]; .id == "maker-prestream" and .builtin) and any(.[]; .id == "maker-ending" and .builtin)'
api "https://$domain/api/generator/themes" | jq -e '
  any(.[]; .id == "arcade-after-hours" and .revision == 2) and
  any(.[]; .id == "neon-night" and .revision == 2) and
  any(.[]; .id == "maker-workshop") and any(.[]; .id == "maker-christmas") and
  any(.[]; .id == "maker-halloween") and any(.[]; .id == "pacman-maze")'

api "https://$domain/api/generator/designs" | jq -e 'length == 0'
design_id=$(api -X POST "https://$domain/api/generator/templates/starter-prestream/designs" \
  -H 'Content-Type: application/json' -H 'X-Restreamer-Control: 1' \
  --data '{"name":"Fresh studio probe","version":1,"theme":{"id":"arcade-after-hours","revision":2}}' | jq -er '.id')
api "https://$domain/api/generator/designs/$design_id" | jq -e '.name == "Fresh studio probe" and (.scenes | length) == 3'

api -X POST "https://$domain/api/destinations" -H 'Content-Type: application/json' -H 'X-Restreamer-Control: 1' \
  --data '{"version":0,"name":"Installer probe","url":"rtmp://127.0.0.1:1936/live","stream_key":"ci-dummy-key"}' | jq -e '.destinations[0].name == "Installer probe"'
before_container=$(compose ps -q restreamer)
before_env=$(sudo sha256sum "$install_directory/.env")
sudo env CURL_CA_BUNDLE="$test_root/ca.pem" bash "$repository/install.sh" --directory "$install_directory"
test "$(compose ps -q restreamer)" = "$before_container"
test "$(sudo sha256sum "$install_directory/.env")" = "$before_env"
sudo docker inspect "$before_container" | jq -e '.[0].HostConfig.RestartPolicy.Name == "unless-stopped" and .[0].HostConfig.PortBindings["8080/tcp"][0].HostIp == "127.0.0.1"'
before_ca=$(sha256sum "$test_root/ca.pem" | cut -d ' ' -f1)
compose restart
sudo env CURL_CA_BUNDLE="$test_root/ca.pem" bash "$repository/install.sh" --directory "$install_directory"
api "https://$domain/api/destinations" | jq -e '.destinations | any(.name == "Installer probe" and .has_key == true)'
api "https://$domain/api/generator/designs/$design_id" | jq -e '.name == "Fresh studio probe" and (.scenes | length) == 3'
sudo docker cp "$(compose ps -q caddy):/data/caddy/pki/authorities/local/root.crt" "$test_root/ca-after.pem"
test "$(sudo sha256sum "$test_root/ca-after.pem" | cut -d ' ' -f1)" = "$before_ca"
echo 'Installer HTTPS, authentication, rerun and persistence smoke checks passed.'
