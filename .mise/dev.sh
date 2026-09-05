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
# THE SECOND FACTOR IS OFF HERE BY DEFAULT. This substrate is thrown away by
# `dev:wipe` and registration is one-shot per user, so every fresh start would
# otherwise mean enrolling an authenticator entry to reach a repository that
# will not outlive the afternoon. Nothing else in the tree turns it off, and
# `dev:totp` runs the same substrate with the factor enforced — which is how a
# change to the door gets tested.
# Not readonly: `dev:totp` is this same substrate with the factor put back.
DISABLE_TOTP="${SUBSTRATE_INSECURE_DISABLE_TOTP:-true}"
# Changelog signing is mandatory and the signing seed seals under the
# credential key, so the dev substrate mints a key once and keeps it beside
# the state it belongs to: `dev:wipe` removes both together. An operator
# command against this substrate reads the same file (dev:status prints the
# path). The env var wins where a shell already carries one.
#
# The key is base64 of 32 bytes, what `openssl rand -base64 32` prints and the
# only shape the server accepts (ADR 0024).
readonly CREDFILE="${STATE}/credential.key"
cred_key() {
	if [ -n "${SUBSTRATE_CREDENTIAL_KEY:-}" ]; then
		echo "$SUBSTRATE_CREDENTIAL_KEY"
		return
	fi
	mkdir -p "$STATE"
	if [ ! -f "$CREDFILE" ]; then
		head -c 32 /dev/urandom | base64 | tr -d '\n' >"$CREDFILE"
	fi
	cat "$CREDFILE"
}
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
#
# The command name is checked, not just the pid. A pid is not an identity —
# the kernel reuses numbers — so a pidfile that outlived a crash would aim
# `dev:stop` at whatever inherited that number. Only our own binary answers.
server_pid() {
	[ -f "$PIDFILE" ] || return 1
	local pid
	pid="$(cat "$PIDFILE")"
	[ -n "$pid" ] || return 1
	[ "$(ps -p "$pid" -o comm= 2>/dev/null)" = "substrate" ] || return 1
	echo "$pid"
}

wait_healthy() {
	local pid
	pid="$(cat "$PIDFILE" 2>/dev/null)"
	for _ in $(seq 1 60); do
		# The pid first: a server that died at boot (say, the port was taken)
		# must not be vouched for by whatever else answers /healthz there.
		# Liveness, not the command name: right after the fork the child is
		# still `env`, and failing on that would kill a healthy start.
		[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || return 1
		if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.5
	done
	return 1
}

# totp_note says which door this substrate is running, every time it starts:
# a factor that is off must never be something you find out by accident.
totp_note() {
	if [ "$DISABLE_TOTP" = "true" ]; then
		echo "  second factor: OFF (username + password; mise run dev:totp enforces it)"
	else
		echo "  second factor: enforced"
	fi
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

# The server takes NO LLM configuration. Completions and embeddings are bought
# through a repository's own `llmprovider` records, so a dev substrate that
# wants either writes one after registering:
#
#   bin/substratectl apply -f - <<'YAML'
#   kind: substrate.reamde.dev/core/llmprovider
#   metadata:
#     id: vectors
#   data:
#     properties:
#       name: vectors
#       wire: openai
#       baseURL: https://api.openai.com/v1
#       apiKey: sk-...
#       embedModel: text-embedding-3-small
#   YAML
#
# The key lands in the repository's sealed store, not in this shell's history
# of exported variables, which is the point of the move.

server_start() {
	if server_pid >/dev/null; then
		echo "dev: already running (pid $(server_pid)); mise run dev:restart"
		return 0
	fi
	# A healthz answer with no pid of ours is a FOREIGN server on the port
	# (another checkout's, usually). Starting would die on bind while the
	# health poll blesses the squatter, so refuse while the port can be moved.
	if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
		echo "dev: something else answers on :${PORT} and it is not this tree's server; stop it or set SUBSTRATE_DEV_PORT" >&2
		return 1
	fi
	mkdir -p "$STATE"
	# env -S is not portable enough to be worth it; the list is short.
	local web=()
	[ -d "$WEB_DIR" ] && web=("WEB_DIR=${WEB_DIR}")
	# Egress stays default-closed; the variable passes through only when the
	# caller set one. The e2e suite needs loopback open, because its
	# llmprovider rows point at a stub the test process hosts.
	local egress=()
	[ -n "${SUBSTRATE_EGRESS_ALLOW:-}" ] && egress=("SUBSTRATE_EGRESS_ALLOW=${SUBSTRATE_EGRESS_ALLOW}")
	nohup env \
		"DATABASE_URL=${DSN}" \
		"PORT=${PORT}" \
		"SUBSTRATE_INVITE_CODE=${INVITE}" \
		"SUBSTRATE_INSECURE_DISABLE_TOTP=${DISABLE_TOTP}" \
		"SUBSTRATE_CREDENTIAL_KEY=$(cred_key)" \
		"LOG_LEVEL=${LOG_LEVEL:-info}" \
		"${web[@]}" \
		"${egress[@]}" \
		bin/substrate >>"$LOGFILE" 2>&1 &
	echo $! >"$PIDFILE"
	if ! wait_healthy; then
		echo "dev: the server did not come up; tail -n 40 ${LOGFILE}" >&2
		tail -n 40 "$LOGFILE" >&2 || true
		# Leave nothing behind: a process that never got healthy still holds
		# the port, and its pidfile would make the next `dev:up` report a
		# running server.
		server_stop >/dev/null
		return 1
	fi
	echo "dev: substrate up (pid $(cat "$PIDFILE")), invite code ${INVITE}"
	totp_note
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
	totp_note
	urls
	local web=()
	[ -d "$WEB_DIR" ] && web=("WEB_DIR=${WEB_DIR}")
	local egress=()
	[ -n "${SUBSTRATE_EGRESS_ALLOW:-}" ] && egress=("SUBSTRATE_EGRESS_ALLOW=${SUBSTRATE_EGRESS_ALLOW}")
	exec env \
		"DATABASE_URL=${DSN}" \
		"PORT=${PORT}" \
		"SUBSTRATE_INVITE_CODE=${INVITE}" \
		"SUBSTRATE_INSECURE_DISABLE_TOTP=${DISABLE_TOTP}" \
		"SUBSTRATE_CREDENTIAL_KEY=$(cred_key)" \
		"LOG_LEVEL=${LOG_LEVEL:-info}" \
		"${web[@]}" \
		"${egress[@]}" \
		bin/substrate
}

cmd_up() {
	db_up
	server_start
}

# cmd_totp is `run` with the second factor ENFORCED — the same database, the
# same users, a door that asks for a code. It is how a change to the door is
# exercised against the shape a deployment actually runs, and it is why the
# default being off costs nothing.
cmd_totp() {
	DISABLE_TOTP=false
	cmd_run
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
	# A foreground `mise run dev` has no pidfile, so server_stop did not touch
	# it — and pulling the database out from under a live server is how you get
	# one that is half migrated. The port still answering is exactly that case.
	if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
		echo "dev: something is still serving :${PORT} — a foreground \`mise run dev\`? stop it first; the database is untouched" >&2
		return 1
	fi
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
		# The RUNNING server's own answer, not what this shell would start one
		# with: `dev` and `dev:totp` differ, so status must report the door
		# that is actually up.
		case "$(curl -fsS "http://127.0.0.1:${PORT}/.well-known/substrate/server.json" 2>/dev/null)" in
		*'"totpRequired":false'*) echo "  second factor: OFF (username + password)" ;;
		*'"totpRequired":true'*) echo "  second factor: enforced" ;;
		esac
		urls
	else
		echo "substrate: stopped"
	fi
	if [ -d "$WEB_DIR" ]; then
		echo "console:   built (served at /)"
	else
		echo "console:   not built (mise run console:build)"
	fi
	if [ -f "$CREDFILE" ]; then
		echo "credential key: ${CREDFILE} (export SUBSTRATE_CREDENTIAL_KEY=\$(cat ${CREDFILE}) for operator commands)"
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
run | totp | up | stop | restart | wipe | status | logs | dsn | psql)
	verb="$1"
	shift
	"cmd_${verb}" "$@"
	;;
*)
	echo "usage: .mise/dev.sh {run|totp|up|stop|restart|wipe|status|logs|dsn|psql}" >&2
	exit 2
	;;
esac
