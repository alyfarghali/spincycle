# dev

Development-only data for a local database. Unlike `storage/migrations/`, nothing here runs automatically, and none of it belongs in production.

Load it after a fresh database (from the repo root):

```sh
docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  psql -U spincycle -d spincycle -v ON_ERROR_STOP=1 < storage/dev/dev_data.sql
```

Each device here also needs a broker credential: `broker/dynsec/device.sh add <fleet> <device>`.
