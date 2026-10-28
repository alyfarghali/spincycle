# deploy

SpinCycle runs on the shared portfolio server (`2.29.125.26`), next to other projects. That server's
layout is managed by the `alyfarghali-server` repo:

- One **edge Caddy** (`edge-caddy-1`) owns ports 80 and 443 for every project, gets the HTTPS
  certificates, and forwards `spincycle.alyfarghali.com` to `spincycle-web:80` on the shared `edge`
  Docker network.
- App code lives in `/srv/src/<app>`, secrets in `/srv/secrets/<app>.env`.

| File | Used |
|---|---|
| `docker-compose.yml` | Everywhere: the same services locally and in production |
| `docker-compose.prod.yml` | Production only: joins `proxy` to the `edge` network as `spincycle-web` |
| `Caddyfile` | SpinCycle's own Caddy, plain HTTP: serves `web/dist`, proxies `/api` and `/health` to `api:8000` |
| `deploy.sh` | From the laptop: rsync to `/srv/src/spincycle`, then `docker compose up -d --build` with both files |

| Service | Reachable from the internet |
|---|---|
| `proxy` | Only through the edge Caddy (HTTPS) |
| `mosquitto` | 8883, TLS with the edge Caddy's certificate for the same domain |
| `ingest`, `api` | No |
| `postgres`, `redis` | No; 127.0.0.1 only |

The repo is private, so the server never clones it: `deploy.sh` copies files with rsync and skips
`firmware/`, `.env` and certificates.

## Deploying

```sh
deploy/deploy.sh
```

New migrations don't run on an existing database; apply them by hand (`storage/README.md`).

## How it was set up

Already done on the current server; repeat only for a new one.

1. DNS A record `spincycle` → server IP.
2. In `alyfarghali-server`: a `spincycle.alyfarghali.com` block in `edge/caddy/Caddyfile` proxying to
   `spincycle-web:80`, then its `./deploy.sh`. The edge Caddy gets the certificate.
3. On the server: `/srv/secrets/spincycle.env` with random passwords (`openssl rand -hex 24`) for
   `MQTT_ADMIN_PASSWORD`, `MQTT_INGEST_PASSWORD`, `MQTT_API_PASSWORD`, `MQTT_LOADGEN_PASSWORD`,
   `POSTGRES_PASSWORD`, plus:
   ```
   COMPOSE_PROJECT_NAME=spincycle
   DOMAIN=spincycle.alyfarghali.com
   CERT_SOURCE=edge-caddy-1
   BROKER_CERT_DIR=/srv/src/spincycle/broker/certs/prod
   POSTGRES_DB=spincycle
   POSTGRES_USER=spincycle
   ```
   Link it: `ln -sfn /srv/secrets/spincycle.env /srv/src/spincycle/.env`. Open the broker port:
   `ufw allow 8883/tcp`.
4. Laptop: `deploy/deploy.sh proxy` (code + SpinCycle's Caddy only).
5. Server, in `/srv/src/spincycle`: `broker/certs/sync-certs.sh`, then `broker/dynsec/bootstrap.sh`.
   Mosquitto must not start before this: if its certificate files don't exist yet, Docker creates
   directories in their place.
6. Laptop: `deploy/deploy.sh`.
7. Server: the daily certificate sync in root's crontab:
   `0 4 * * * /srv/src/spincycle/broker/certs/sync-certs.sh`.

## Adding a device

On the server, in `/srv/src/spincycle`: insert the `fleets`/`devices` rows in Postgres, then
`broker/dynsec/device.sh add <fleet> <device>`. The password is shown once; write it into the device
with `firmware/tools/provision.py`, broker URL `mqtts://spincycle.alyfarghali.com:8883`.

## Checking it

```sh
ssh root@2.29.125.26
cd /srv/src/spincycle
docker compose --env-file .env -f deploy/docker-compose.yml ps
docker compose --env-file .env -f deploy/docker-compose.yml logs -f ingest
```

The broker should present a valid certificate:

```sh
openssl s_client -connect spincycle.alyfarghali.com:8883 -servername spincycle.alyfarghali.com </dev/null | grep "Verify return code"
```
