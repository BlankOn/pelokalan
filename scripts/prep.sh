#!/usr/bin/env bash
# Prepares everything `make run` needs on a development machine. Safe to run again at any time:
#   - checks that docker and go are installed
#   - creates .env from .env.example when there is none (an existing .env is never overwritten)
#   - starts PostgreSQL in docker (container pelokalan-postgres), creating it with a named volume
#     (pelokalan-pgdata) the first time so data survives restarts and re-runs
#   - when the database port is taken by something else, picks the next free port and writes it to .env
#     as DB_PORT (an existing container keeps its data volume when it moves to the new port)
#   - waits until the database accepts connections and downloads the Go modules
set -euo pipefail
cd "$(dirname "$0")/.."

CONTAINER=${POSTGRES_CONTAINER_NAME:-pelokalan-postgres}
IMAGE=${POSTGRES_IMAGE_TAG:-postgres:16}
VOLUME=${POSTGRES_VOLUME:-pelokalan-pgdata}

say() { printf '\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null || fail "docker is not installed (https://docs.docker.com/get-docker/)"
docker info >/dev/null 2>&1 || fail "docker is installed but not running, or this user may not use it"
command -v go >/dev/null || fail "go is not installed (https://go.dev/dl/, version 1.24 or newer)"

if [ ! -f .env ]; then
    cp .env.example .env
    say "Created .env from .env.example"
fi

# read a setting from .env (last value wins), with a default
env_get() { local v; v=$(grep -E "^$1=" .env | tail -n1 | cut -d= -f2- || true); echo "${v:-$2}"; }
env_set() {
    if grep -qE "^$1=" .env; then sed -i.bak -E "s|^$1=.*|$1=$2|" .env && rm -f .env.bak; else echo "$1=$2" >> .env; fi
}

DB_USER=$(env_get DB_USER postgres)
DB_PASSWORD=$(env_get DB_PASSWORD postgres)
DB_NAME=$(env_get DB_NAME pelokalan)
PORT=$(env_get DB_PORT 5432)

port_busy() { # is something listening on the port?
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3>&- 3<&-; return 0; } || return 1
}
container_port() { docker port "$CONTAINER" 5432/tcp 2>/dev/null | head -n1 | sed -E 's/.*:([0-9]+)$/\1/'; }
free_port_from() { local p=$1; while port_busy "$p"; do p=$((p + 1)); done; echo "$p"; }

create_container() { # $1 = host port, $2 = volume (name or path)
    docker run -d --name "$CONTAINER" \
        -e POSTGRES_DB="$DB_NAME" -e POSTGRES_USER="$DB_USER" -e POSTGRES_PASSWORD="$DB_PASSWORD" \
        -p "127.0.0.1:$1:5432" -v "$2:/var/lib/postgresql/data" "$IMAGE" >/dev/null
}

if docker container inspect "$CONTAINER" >/dev/null 2>&1; then
    running=$(docker container inspect -f '{{.State.Running}}' "$CONTAINER")
    if [ "$running" = "true" ]; then
        PORT=$(container_port)
        say "PostgreSQL is already running (container $CONTAINER, port $PORT)"
    else
        want=$(docker container inspect -f '{{range $p, $b := .HostConfig.PortBindings}}{{(index $b 0).HostPort}}{{end}}' "$CONTAINER")
        if [ -n "$want" ] && port_busy "$want"; then
            # its port is taken by something else: move it to a free port, keeping its data volume
            data=$(docker container inspect -f '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' "$CONTAINER")
            [ -n "$data" ] || fail "container $CONTAINER has no data volume to keep; remove it (docker rm $CONTAINER) and run make prep again"
            PORT=$(free_port_from $((want + 1)))
            say "Port $want is taken by another program: moving $CONTAINER to port $PORT (data volume $data is kept)"
            docker rm "$CONTAINER" >/dev/null
            create_container "$PORT" "$data"
        else
            say "Starting PostgreSQL (container $CONTAINER)"
            docker start "$CONTAINER" >/dev/null
            PORT=$(container_port)
        fi
    fi
else
    if port_busy "$PORT"; then
        busy=$PORT
        PORT=$(free_port_from $((PORT + 1)))
        say "Port $busy is taken by another program: using port $PORT for PostgreSQL"
    fi
    say "Creating PostgreSQL container $CONTAINER ($IMAGE, port $PORT, volume $VOLUME)"
    docker volume create "$VOLUME" >/dev/null
    create_container "$PORT" "$VOLUME"
fi

if [ "$(env_get DB_PORT 5432)" != "$PORT" ] || ! grep -qE '^DB_PORT=' .env; then
    env_set DB_PORT "$PORT"
    say "Set DB_PORT=$PORT in .env"
fi

say "Waiting for PostgreSQL to accept connections"
for _ in $(seq 1 60); do
    if docker exec "$CONTAINER" pg_isready -q -U "$DB_USER" -d "$DB_NAME" 2>/dev/null; then ready=1; break; fi
    sleep 1
done
[ "${ready:-}" = 1 ] || fail "PostgreSQL did not become ready; see: docker logs $CONTAINER"

say "Downloading Go modules"
go mod download

site=$(env_get SITE_BASE_URL http://localhost:8080)
cat <<MSG

Ready. Start the app with:

    make run

then open $site
Migrations run on start. Without MAILTRAP_API_TOKEN in .env, sign-in codes are printed in the
terminal running the app. EMAIL_WHITELIST in .env limits who may sign in (empty = anyone).
MSG
