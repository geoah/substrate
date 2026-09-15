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
# All of its state is disposable and lives in two places: a DATABASE OF THIS
# TREE'S OWN inside the shared container, and .dev/ (the pid, the log, the
# credential key and the data root). `wipe` removes both, which is the only way
# to get a FRESH substrate: registration is one-shot per user and there is no
# unregister, and a data root that outlives its database is imported at the
# next boot. The container and its volume outlive a wipe, because every other
# checkout and worktree on this box keeps a database in them; `wipe-all`
# (`mise run dev:wipe:all`) is the one that takes those too.
set -euo pipefail

cd "$(dirname "$0")/.."

# The same image compose.yaml runs, so a bug that needs pgvector shows up here.
readonly PG_IMAGE="pgvector/pgvector:pg16"
readonly CONTAINER="${SUBSTRATE_DEV_DB_CONTAINER:-substrate-dev-db}"
readonly VOLUME="${CONTAINER}-data"
# 5433, not 5432: a Postgres already installed on the box keeps its own port.
readonly DB_PORT="${SUBSTRATE_DEV_DB_PORT:-5433}"
readonly PORT="${SUBSTRATE_DEV_PORT:-8080}"
# The database the container bootstraps with, and the only one this script
# connects to as an administrator: the per-tree databases are created and
# dropped from it, so `wipe` never has to drop the database it is speaking to.
readonly BOOTSTRAP_DB="substrate"
# ONE DATABASE PER TREE, inside the one container. Every checkout and worktree
# on a box used to share a single `substrate` database while each kept a data
# root of its own — three servers, three boot upgrades, three trigger
# dispatchers over one set of repositories, which is how #539 happened. The
# name is derived from the TREE ROOT'S BASENAME and is readable on purpose
# (`substrate_substrate` for a checkout at src/substrate, `substrate_issue_554`
# for a worktree named issue-554), so `psql -l` says which tree owns what and
# an operator hat never has to guess. TWO TREES WITH THE SAME BASENAME SHARE A
# DATABASE: name worktrees distinctly, or set SUBSTRATE_DEV_DB_NAME. Every
# start and `dev:status` print the name so the collision is visible.
dev_db_name() {
	local base
	base="$(basename "$(pwd)")"
	# Lowercase and fold everything outside the identifier alphabet: an
	# unquoted mixed-case or hyphenated name is not the name Postgres stores.
	# printf, not echo, so tr never sees the trailing newline and turns it
	# into an underscore.
	base="$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_' '_')"
	# 63 bytes is Postgres's identifier limit and it TRUNCATES SILENTLY past
	# it, which would make two long-named trees one database without saying
	# so. Cut here instead, where the name is still printed.
	printf 'substrate_%.53s\n' "$base"
}
DB_NAME="${SUBSTRATE_DEV_DB_NAME:-$(dev_db_name)}"
readonly DB_NAME
readonly DSN="postgres://postgres:postgres@127.0.0.1:${DB_PORT}/${DB_NAME}?sslmode=disable"
readonly STATE=".dev"
readonly PIDFILE="${STATE}/substrate.pid"
readonly LOGFILE="${STATE}/substrate.log"
# NO INVITE CODE BY DEFAULT: the door reads none, so registering here is a
# repository name and a password. Same as compose.yaml, so a walkthrough written
# against one path works against the other. Set one to test the gate
# (`test:e2e` does).
readonly INVITE="${SUBSTRATE_INVITE_CODE:-}"
invite_words() { [ -n "$INVITE" ] && echo "invite code ${INVITE}" || echo "no invite code"; }
# THE SECOND FACTOR IS OFF HERE BY DEFAULT. This substrate is thrown away by
# `dev:wipe` and registration is one-shot per user, so every fresh start would
# otherwise mean enrolling an authenticator entry to reach a repository that
# will not outlive the afternoon. `dev:totp` runs the same substrate with the
# factor enforced — which is how a change to the door gets tested.
# Not readonly: `dev:totp` is this same substrate with the factor put back.
DISABLE_TOTP="${SUBSTRATE_INSECURE_DISABLE_TOTP:-true}"
# The credential key wraps each repository's DEK and a host without one refuses
# to boot, so the dev substrate mints a key once and keeps it beside the state
# it belongs to: `dev:wipe` removes both together. An operator command that
# writes sealed material reads the same file (dev:status prints the path). The
# env var wins where a shell already carries one.
#
# The key is base64 of 32 bytes, what `openssl rand -base64 32` prints and the
# only shape the server accepts (ADR 0024).
readonly CREDFILE="${STATE}/credential.key"
# The data root: one directory per repository holding its changelog segments,
# sealed files and blob bytes. The server requires an ABSOLUTE path (a relative
# one would follow whichever directory the server was started from), so it is
# resolved from the tree root the `cd` above landed in. It lives under .dev so
# `dev:wipe` removes it with the database it indexes.
DATA_ROOT="$(pwd)/${STATE}/data"
readonly DATA_ROOT
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

# db_start brings the SHARED container up and waits for its socket. It is
# shared by every tree on the box, so nothing here stops it, recreates it or
# touches its volume: only `wipe-all` does.
db_start() {
	case "$(db_state)" in
	running) ;;
	# A dev database is thrown away with `dev:wipe`, so it flushes nothing:
	# fsync and the WAL flush cost most of a write and protect a power loss
	# nobody here needs to survive. The flags ride the container's command,
	# so an existing container keeps its old ones until it is recreated.
	absent)
		docker run -d \
			--name "$CONTAINER" \
			-e POSTGRES_PASSWORD=postgres \
			-e "POSTGRES_DB=${BOOTSTRAP_DB}" \
			-v "${VOLUME}:/var/lib/postgresql/data" \
			-p "127.0.0.1:${DB_PORT}:5432" \
			"$PG_IMAGE" \
			-c fsync=off -c synchronous_commit=off -c full_page_writes=off >/dev/null
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
		if docker exec "$CONTAINER" pg_isready -U postgres -d "$BOOTSTRAP_DB" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	echo "dev: postgres did not become ready; docker logs ${CONTAINER}" >&2
	return 1
}

# db_exists answers from the catalog rather than by connecting to the database
# itself: a connection attempt cannot tell "no such database" from "not ready
# yet", and one of those is a create and the other is a wait. It has THREE
# answers, not two: 0 present, 1 absent, 2 the probe itself failed (Postgres
# out of connections, restarting after pg_isready). A caller that read a
# failed probe as "absent" would drop nothing and still throw the data root
# and the credential key away, so the failure is its own exit code.
db_exists() {
	local out
	if ! out="$(docker exec "$CONTAINER" psql -U postgres -d "$BOOTSTRAP_DB" -tAc \
		"SELECT 1 FROM pg_database WHERE datname = '${DB_NAME}'" 2>/dev/null)"; then
		return 2
	fi
	[ "$out" = "1" ]
}

# db_ensure creates THIS TREE'S database on first use. The container ships one
# database of its own (BOOTSTRAP_DB) and no tree uses it: it is only the
# administrative door the create and the drop are issued through.
db_ensure() {
	if db_exists; then
		return 0
	fi
	docker exec "$CONTAINER" createdb -U postgres "$DB_NAME"
	echo "dev: database ${DB_NAME} created (one per tree, named after this directory)"
}

db_up() {
	db_start
	db_ensure
}

# db_drop drops THIS TREE'S database and nothing else: the container, its
# volume and every other tree's database survive, because they are not this
# tree's to throw away.
db_drop() {
	# A CONTAINER THAT IS GONE DOES NOT MEAN THE DATABASE IS. `docker rm`
	# without -v leaves the named volume behind, and the volume is where the
	# database lives; the container is just a process in front of it. Reading
	# an absent container as "nothing to drop" would delete this tree's data
	# root and its credential key and leave its database sitting in the
	# volume, so the next start would reattach it under a freshly minted key
	# and every repository in it would refuse to open — the shape a lost keys
	# volume has, from a command whose whole job was to leave nothing behind.
	# So only NEITHER of them is absent.
	if [ "$(db_state)" = "absent" ] && ! docker volume inspect "$VOLUME" >/dev/null 2>&1; then
		echo "dev: no ${CONTAINER} container and no ${VOLUME} volume, so there is no ${DB_NAME} to drop"
		return 0
	fi
	if [ "$(db_state)" = "absent" ]; then
		echo "dev: ${CONTAINER} is gone but volume ${VOLUME} is not, so ${DB_NAME} may still be in it; starting the container to drop it"
	fi
	db_start
	local probe=0
	db_exists || probe=$?
	case "$probe" in
	0) ;;
	1)
		echo "dev: database ${DB_NAME} was already gone"
		return 0
		;;
	*)
		echo "dev: could not ask ${CONTAINER} whether ${DB_NAME} exists; nothing was wiped" >&2
		return 1
		;;
	esac
	# A backend still on the database refuses the drop, and after the healthz
	# guard above the only ones left are connections a dead server never
	# closed. Terminating them is what makes the wipe idempotent.
	docker exec "$CONTAINER" psql -U postgres -d "$BOOTSTRAP_DB" -tAc \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${DB_NAME}'" >/dev/null
	docker exec "$CONTAINER" dropdb -U postgres --if-exists "$DB_NAME"
	echo "dev: database ${DB_NAME} dropped (${CONTAINER} and every other tree's database are untouched)"
}

# db_nuke removes the shared container and its volume — EVERY TREE'S DATABASE
# ON THIS BOX, not just this one's. Only `wipe-all` calls it.
db_nuke() {
	if [ "$(db_state)" != "absent" ]; then
		docker rm -f "$CONTAINER" >/dev/null
	fi
	docker volume rm "$VOLUME" >/dev/null 2>&1 || true
	echo "dev: container removed (${CONTAINER}, volume ${VOLUME}) — every tree's dev database went with it"
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
#
# The BASENAME of what ps prints, because the two platforms print different
# things: Linux gives the short command name (`substrate`) and macOS gives the
# path the server was started with (`bin/substrate`). Comparing the whole
# string matched neither reliably, so every `dev:status` said "stopped" and
# `dev:stop` stopped nothing on a Mac.
server_pid() {
	[ -f "$PIDFILE" ] || return 1
	local pid comm
	pid="$(cat "$PIDFILE" 2>/dev/null || true)"
	[ -n "$pid" ] || return 1
	comm="$(ps -p "$pid" -o comm= 2>/dev/null || true)"
	[ "${comm##*/}" = "substrate" ] || return 1
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

# db_note names this tree's database every time the substrate starts. The name
# is derived from the directory, so two worktrees with the same basename would
# otherwise share one database silently; printing it is the whole collision
# check.
db_note() {
	echo "  database: ${DB_NAME} (this tree's own, in ${CONTAINER})"
}

# totp_note says which door this substrate is running, every time it starts:
# a factor that is off must never be something you find out by accident.
totp_note() {
	if [ "$DISABLE_TOTP" = "true" ]; then
		echo "  second factor: OFF (repository name + password; mise run dev:totp enforces it)"
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
# through a repository's own `llm/provider` records, so a dev substrate that
# wants either writes one after registering:
#
#   bin/substratectl apply -f - <<'YAML'
#   kind: substrate.reamde.dev/llm/provider
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
		echo "dev: something else answers on :${PORT} and it is not this tree's server — another tree's dev server? stop it or set SUBSTRATE_DEV_PORT" >&2
		return 1
	fi
	mkdir -p "$STATE"
	# env -S is not portable enough to be worth it; the list is short.
	local web=()
	[ -d "$WEB_DIR" ] && web=("WEB_DIR=${WEB_DIR}")
	# Egress stays default-closed; the variable passes through only when the
	# caller set one. The e2e suite needs loopback open, because its
	# llm/provider rows point at a stub the test process hosts.
	local egress=()
	[ -n "${SUBSTRATE_EGRESS_ALLOW:-}" ] && egress=("SUBSTRATE_EGRESS_ALLOW=${SUBSTRATE_EGRESS_ALLOW}")
	nohup env \
		"DATABASE_URL=${DSN}" \
		"PORT=${PORT}" \
		"SUBSTRATE_INVITE_CODE=${INVITE}" \
		"SUBSTRATE_INSECURE_DISABLE_TOTP=${DISABLE_TOTP}" \
		"SUBSTRATE_CREDENTIAL_KEY=$(cred_key)" \
		"SUBSTRATE_DATA_ROOT=${DATA_ROOT}" \
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
	echo "dev: substrate up (pid $(cat "$PIDFILE")), $(invite_words)"
	db_note
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
	echo "dev: substrate on :${PORT}, $(invite_words) (ctrl-c to stop)"
	db_note
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
		"SUBSTRATE_DATA_ROOT=${DATA_ROOT}" \
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

# cmd_stop stops THIS TREE'S SERVER AND NOTHING ELSE. The container is shared
# by every checkout and worktree on the box, so stopping it here would take
# somebody else's substrate down with this one; `wipe-all` is the only path
# that touches it.
cmd_stop() {
	server_stop
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
	db_drop
	# The data root goes with the database: a repository directory left behind
	# would be imported into the fresh database at the next boot, and the
	# substrate would not be fresh.
	rm -rf "$DATA_ROOT"
	rm -rf "$STATE"
	echo "dev: wiped (${DB_NAME} and ${DATA_ROOT}); the next start is a fresh substrate with no users"
}

# cmd_wipe_all is `wipe` plus the SHARED CONTAINER: every tree's dev database
# on this box, not only this one's. It is the way to reclaim the volume, and
# the reason it is a separate verb is that `dev:wipe` is typed many times a day
# and must never be able to delete another worktree's substrate.
cmd_wipe_all() {
	cmd_wipe
	db_nuke
}

cmd_status() {
	# The container's state and THIS TREE'S database, separately: the first is
	# shared with every other checkout on the box and the second is not, and a
	# running container with no database of this tree's is an ordinary
	# post-wipe state, not a fault.
	local own="absent"
	[ "$(db_state)" = "running" ] && db_exists && own="present"
	echo "container: $(db_state)  (${CONTAINER} on 127.0.0.1:${DB_PORT}, shared by every tree)"
	echo "database:  ${own}  (${DB_NAME} — this tree's own; two trees with the same directory name would share it, so set SUBSTRATE_DEV_DB_NAME)"
	echo "dsn:       ${DSN}"
	if server_pid >/dev/null; then
		local health="unhealthy"
		curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1 && health="healthy"
		echo "substrate: running (pid $(server_pid)), ${health}"
		# The RUNNING server's own answer, not what this shell would start one
		# with: `dev` and `dev:totp` differ, so status must report the door
		# that is actually up.
		local disc
		disc="$(curl -fsS "http://127.0.0.1:${PORT}/.well-known/substrate/server.json" 2>/dev/null)"
		case "$disc" in
		*'"inviteRequired":false'*) echo "  invite code:   none (anyone who reaches this port may register)" ;;
		*'"inviteRequired":true'*) echo "  invite code:   required" ;;
		esac
		case "$disc" in
		*'"totpRequired":false'*) echo "  second factor: OFF (repository name + password)" ;;
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
	echo "data root: ${DATA_ROOT} (export SUBSTRATE_DATA_ROOT=${DATA_ROOT} for operator commands)"
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


case "${1:-}" in
run | totp | up | stop | restart | wipe | status | logs | dsn)
	verb="$1"
	shift
	"cmd_${verb}" "$@"
	;;
# A hyphen, not the task's colon: a function name is what the dispatch above
# builds, and `cmd_wipe:all` is not one.
wipe-all)
	shift
	cmd_wipe_all "$@"
	;;
*)
	echo "usage: .mise/dev.sh {run|totp|up|stop|restart|wipe|wipe-all|status|logs|dsn}" >&2
	exit 2
	;;
esac
