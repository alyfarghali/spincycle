#!/usr/bin/env bash
# Creates a local development CA and a broker certificate signed by it, for the TLS door on 8883.
#
# Usage: broker/certs/make-dev-certs.sh [extra-hostname-or-ip ...]
#   Pass your laptop's LAN IP so an ESP32 on the same network can connect during development.
set -euo pipefail

out="${OUT_DIR:-$(cd "$(dirname "$0")" && pwd)/dev}"
mkdir -p "$out"
cd "$out"

# Local CA, created once and reused. Test clients and dev firmware trust ca.pem.
if [[ ! -f ca.pem ]]; then
  openssl req -x509 -new -nodes -newkey rsa:2048 -sha256 -days 825 \
    -keyout ca.key -out ca.pem -subj "/CN=SpinCycle Dev CA" 2>/dev/null
  chmod 600 ca.key
fi

# Names the broker certificate is valid for. Clients check that the name they
# connected to is in this list (hostname verification).
sans="DNS:localhost,DNS:mosquitto,IP:127.0.0.1"
for name in "$@"; do
  if [[ $name =~ ^[0-9.]+$ ]]; then sans+=",IP:$name"; else sans+=",DNS:$name"; fi
done

openssl req -new -nodes -newkey rsa:2048 -keyout privkey.pem -out broker.csr \
  -subj "/CN=mosquitto" 2>/dev/null
openssl x509 -req -in broker.csr -CA ca.pem -CAkey ca.key -CAcreateserial \
  -days 397 -sha256 -out fullchain.pem \
  -extfile <(printf "subjectAltName=%s\nextendedKeyUsage=serverAuth\n" "$sans") 2>/dev/null
rm -f broker.csr

# The container's mosquitto user must be able to read the key. Dev only.
chmod 644 privkey.pem

echo "Broker cert: $out/fullchain.pem (valid for $sans)"
echo "Clients trust: $out/ca.pem"
