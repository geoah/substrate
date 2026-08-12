#!/usr/bin/env bash
#
# The dev substrate: one throwaway Postgres in Docker and the server built from
# this tree. Every `mise run dev:*` task is one subcommand here, so .mise.toml
# stays a list of names and the shape of the loop lives in one file.
#
# This is the LOCAL box and only the local box. Nothing here runs in CI or in a
# deployment: `docker compose up` is the demo, this is the thing you keep open
# while you work — the server restarts in a second because it is a binary from
# the tree, not an image.
#
# All of its state is disposable and lives in two places: the container plus
# its volume, and .dev/ (the pid and the log). `wipe` removes both, which is
# the only way to get a FRESH substrate — registration is one-shot per user and
# there is no unregister.
set -euo pipefail

cd "$(dirname "$0")/.."

# The same image compose.yaml runs, so a bug that needs pgvector shows up here.
readonly PG_IMAGE="pgvector/pgvector:pg16"
readonly CONTAINER="${SUBSTRATE_DEV_DB_CONTAINER:-substrate-dev-db}"
readonly VOLUME="${CONTAINER}-data"
# 5433, not 5432: a Postgres already installed on the box keeps its own port.
readonly DB_PORT="${SUBSTRATE_DEV_DB_PORT:-5433}"
readonly PORT="${SUBSTRATE_DEV_PORT:-8080}"
readonly DSN="postgres://postgres:postgres@127.0.0.1:${DB_PORT}/substrate?sslmode=disable"
readonly STATE=".dev"
readonly PIDFILE="${STATE}/substrate.pid"
readonly LOGFILE="${STATE}/substrate.log"
# The same code compose.yaml defaults to, so a walkthrough written against one
# path works against the other.
readonly INVITE="${SUBSTRATE_INVITE_CODE:-let-me-in}"
# Built by `mise run console:build`. Absent means the server serves no console
# at / — the API is still whole, and `mise run console:dev` proxies to it.
readonly WEB_DIR="web/console/dist"

# ---------------------------------------------------------------------------
# the database
# ---------------------------------------------------------------------------

# db_state is one of docker's own status words, or `absent`. It reads the
# status through a variable rather than piping the failure to a default:
# `docker inspect` on a missing container prints an EMPTY LINE to stdout before
# failing, and that line would ride along in front of the default.
db_state() {
	local status
	status="$(docker inspect -f '{{.State.Status}}' "$CONTAINER" 2>/dev/null)" || status=""
	echo "${status:-absent}"
}

db_up() {
	case "$(db_state)" in
	running) ;;
	absent)
		docker run -d \
			--name "$CONTAINER" \
			-e POSTGRES_PASSWORD=postgres \
			-e POSTGRES_DB=substrate \
			-v "${VOLUME}:/var/lib/postgresql/data" \
			-p "127.0.0.1:${DB_PORT}:5432" \
			"$PG_IMAGE" >/dev/null
		echo "dev: postgres started (${CONTAINER} on 127.0.0.1:${DB_PORT})"
		;;
	*)
		docker start "$CONTAINER" >/dev/null
		echo "dev: postgres restarted (${CONTAINER} on 127.0.0.1:${DB_PORT})"
		;;
	esac
	# The server's first act is a migration, so waiting here is what keeps a
	# start from racing an empty socket.
	for _ in $(seq 1 60); do
		if docker exec "$CONTAINER" pg_isready -U postgres -d substrate >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	echo "dev: postgres did not become ready; docker logs ${CONTAINER}" >&2
	return 1
}

db_stop() {
	if [ "$(db_state)" = "running" ]; then
		docker stop "$CONTAINER" >/dev/null
		echo "dev: postgres stopped"
	fi
}

db_wipe() {
	if [ "$(db_state)" != "absent" ]; then
		docker rm -f "$CONTAINER" >/dev/null
	fi
	docker volume rm "$VOLUME" >/dev/null 2>&1 || true
	echo "dev: database removed (${CONTAINER}, volume ${VOLUME})"
}

# ---------------------------------------------------------------------------
# the server
# ---------------------------------------------------------------------------

# server_pid prints the pid of a LIVE background server and fails if there is
# none; a pidfile left by a crash is not a running server.
server_pid() {
	[ -f "$PIDFILE" ] || return 1
	local pid
	pid="$(cat "$PIDFILE")"
	[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || return 1
	echo "$pid"
}

wait_healthy() {
	for _ in $(seq 1 60); do
		if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.5
	done
	return 1
}

urls() {
	echo "  http://localhost:${PORT}"
	# The tailnet address, when there is one: this is how the box is reached
	# from another machine. The server binds every interface, so the only thing
	# to print is the address the tailnet already assigned.
	local ip
	if command -v tailscale >/dev/null 2>&1 && ip="$(tailscale ip -4 2>/dev/null | head -1)" && [ -n "$ip" ]; then
		echo "  http://${ip}:${PORT}  (tailnet)"
	fi
}

server_start() {
	if server_pid >/dev/null; then
		echo "dev: already running (pid $(server_pid)); mise run dev:restart"
		return 0
	fi
	mkdir -p "$STATE"
	# env -S is not portable enough to be worth it; the list is short.
	local web=()
	[ -d "$WEB_DIR" ] && web=("WEB_DIR=${WEB_DIR}")
	nohup env \
		"DATABASE_URL=${DSN}" \
		"PORT=${PORT}" \
		"SUBSTRATE_INVITE_CODE=${INVITE}" \
		"LOG_LEVEL=${LOG_LEVEL:-info}" \
		"${web[@]}" \
		bin/substrate >>"$LOGFILE" 2>&1 &
	echo $! >"$PIDFILE"
	if ! wait_healthy; then
		echo "dev: the server did not come up; tail -n 40 ${LOGFILE}" >&2
		tail -n 40 "$LOGFILE" >&2 || true
		return 1
	fi
	echo "dev: substrate up (pid $(cat "$PIDFILE")), invite code ${INVITE}"
	urls
	[ -d "$WEB_DIR" ] || echo "  (no console: mise run console:build, then mise run dev:restart)"
	echo "  logs: mise run dev:logs"
}

server_stop() {
	local pid
	if ! pid="$(server_pid)"; then
		rm -f "$PIDFILE"
		return 0
	fi
	kill "$pid" 2>/dev/null || true
	for _ in $(seq 1 20); do
		kill -0 "$pid" 2>/dev/null || break
		sleep 0.5
	done
	kill -9 "$pid" 2>/dev/null || true
	rm -f "$PIDFILE"
	echo "dev: substrate stopped"
}

# ---------------------------------------------------------------------------
# the subcommands, one per mise task
# ---------------------------------------------------------------------------

cmd_run() {
	if server_pid >/dev/null; then
		echo "dev: a background server is already on :${PORT} — mise run dev:stop first" >&2
		return 1
	fi
	db_up
	echo "dev: substrate on :${PORT}, invite code ${INVITE} (ctrl-c to stop)"
	urls
	local web=()
	[ -d "$WEB_DIR" ] && web=("WEB_DIR=${WEB_DIR}")
	exec env \
		"DATABASE_URL=${DSN}" \
		"PORT=${PORT}" \
		"SUBSTRATE_INVITE_CODE=${INVITE}" \
		"LOG_LEVEL=${LOG_LEVEL:-info}" \
		"${web[@]}" \
		bin/substrate
}

cmd_up() {
	db_up
	server_start
}

cmd_stop() {
	server_stop
	db_stop
}

cmd_restart() {
	server_stop
	db_up
	server_start
}

cmd_wipe() {
	server_stop
	db_wipe
	rm -rf "$STATE"
	echo "dev: wiped — the next start is a fresh substrate with no users"
}

cmd_status() {
	echo "database:  $(db_state)  (${DSN})"
	if server_pid >/dev/null; then
		local health="unhealthy"
		curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1 && health="healthy"
		echo "substrate: running (pid $(server_pid)), ${health}"
		urls
	else
		echo "substrate: stopped"
	fi
	if [ -d "$WEB_DIR" ]; then
		echo "console:   built (served at /)"
	else
		echo "console:   not built (mise run console:build)"
	fi
}

cmd_logs() {
	[ -f "$LOGFILE" ] || {
		echo "dev: no log yet — mise run dev:up" >&2
		return 1
	}
	tail -n 100 -f "$LOGFILE"
}

cmd_dsn() { echo "$DSN"; }

cmd_psql() { exec docker exec -it "$CONTAINER" psql -U postgres -d substrate "$@"; }

case "${1:-}" in
run | up | stop | restart | wipe | status | logs | dsn | psql)
	verb="$1"
	shift
	"cmd_${verb}" "$@"
	;;
*)
	echo "usage: .mise/dev.sh {run|up|stop|restart|wipe|status|logs|dsn|psql}" >&2
	exit 2
	;;
esac
