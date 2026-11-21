#!/usr/bin/env bash
#
# The dev server Substrate Today is tried against: this tree's `mise run
# dev:*` substrate, with the packages the app reads added and two
# repositories registered. One holds the invented day (seed.py); the other is
# empty, for rehearsing a migration into it.
#
#   apps/today/dev/server.sh up        start it; on a fresh one, set it up and seed the day
#   apps/today/dev/server.sh restart   rebuild (kinds/ is embedded in the binary) and add its packages again
#   apps/today/dev/server.sh reset     drop the database and start over: new repositories, new tokens
#   apps/today/dev/server.sh stop
#
# What it leaves in .dev/ (ignored by git): today-dev.env with the server,
# both repositories and a token for each, and a substratectl config for each.
#
# It keeps a Postgres container of its own and port 8098, so it never shares
# a database server's connections with the other trees' dev servers. Both are
# overridable as `mise run dev:up`'s are (SUBSTRATE_DEV_PORT,
# SUBSTRATE_DEV_DB_CONTAINER, SUBSTRATE_DEV_DB_PORT).
set -euo pipefail

cd "$(dirname "$0")/../../.."

export SUBSTRATE_DEV_PORT="${SUBSTRATE_DEV_PORT:-8098}"
export SUBSTRATE_DEV_DB_CONTAINER="${SUBSTRATE_DEV_DB_CONTAINER:-substrate-dev-db-today}"
export SUBSTRATE_DEV_DB_PORT="${SUBSTRATE_DEV_DB_PORT:-5443}"
# The tasks package is published by an app, not the binary: a local server
# reads its authority document from a file (apps/tasks/README.md).
export SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json"

readonly SERVER="http://localhost:${SUBSTRATE_DEV_PORT}"
readonly ENV_FILE=".dev/today-dev.env"
readonly PASSWORD="devpassword1"
readonly PACKAGES=(routines health tasks today)
# The ones the binary ships, which adding again brings up to the tree. The
# tasks package comes from its authority document, where a version's bytes
# never change: after an edit to apps/tasks/package under a version this
# server already holds, only `reset` takes it.
readonly SHIPPED=(routines health today)

authority() {
	.mise/python.sh .mise/closures.py --out .dev/authority --authority substrate.reamde.dev tasks >/dev/null
}

ctl() {
	local config="$1"
	shift
	SUBSTRATECTL_CONFIG="$PWD/.dev/$config" bin/substratectl "$@"
}

token() {
	awk '$1 == "token:" { print $2; exit }' ".dev/$1"
}

add_packages() {
	local config="$1" package
	shift
	for package in "$@"; do
		ctl "$config" add "substrate.reamde.dev/$package" >/dev/null
	done
}

# The dev server holds no model key, so a run of the preparer would only
# fail. An install writes the shipped trigger whole, enabled, so it is turned
# off again after every add. TODAY_DEV_ASSISTANT=1 leaves it on.
assistant_off() {
	[ "${TODAY_DEV_ASSISTANT:-}" = 1 ] && return 0
	local config
	for config in ctl.yaml ctl-rehearsal.yaml; do
		curl -fsS -o /dev/null -X PATCH \
			-H "Authorization: Bearer $(token "$config")" -H "Content-Type: application/json" \
			-d '{"properties": {"enabled": false}}' \
			"${SERVER}/api/v1/substrate.reamde.dev/core/trigger/today-assistant-on-preparation"
	done
}

setup() {
	mise run build:cli >/dev/null
	local name config
	for name in day rehearsal; do
		config="ctl-$name.yaml"
		[ "$name" = day ] && config="ctl.yaml"
		printf '%s\n' "$PASSWORD" | ctl "$config" register --server "$SERVER" --repository "$name" --skip-totp --password-stdin >/dev/null
		add_packages "$config" "${PACKAGES[@]}"
	done
	# The day's meetings need a calendar; the rehearsal repository takes
	# whatever a migration brings.
	ctl ctl.yaml apply -f apps/today/dev/calendar.yaml >/dev/null
	cat >"$ENV_FILE" <<ENV
# The Today dev server (apps/today/dev/README.md). Disposable: \`apps/today/dev/server.sh reset\` makes new tokens.
TODAY_DEV_SERVER=${SERVER}
# The invented day (apps/today/dev/seed.py).
TODAY_DEV_REPOSITORY=day.localhost
TODAY_DEV_TOKEN=$(token ctl.yaml)
TODAY_DEV_CTL=$PWD/.dev/ctl.yaml
# An empty repository with the same packages, for rehearsing a migration.
TODAY_REHEARSAL_REPOSITORY=rehearsal.localhost
TODAY_REHEARSAL_TOKEN=$(token ctl-rehearsal.yaml)
TODAY_REHEARSAL_CTL=$PWD/.dev/ctl-rehearsal.yaml
# Both passwords are ${PASSWORD}; the second factor is off.
ENV
	assistant_off
	python3 apps/today/dev/seed.py
	echo "today dev: ${SERVER}, repositories day.localhost (seeded) and rehearsal.localhost (empty); tokens in ${ENV_FILE}"
}

case "${1:-}" in
up)
	mkdir -p .dev
	authority
	mise run dev:up
	[ -f "$ENV_FILE" ] || setup
	;;
restart)
	authority
	mise run dev:restart
	mise run build:cli >/dev/null
	add_packages ctl.yaml "${SHIPPED[@]}"
	add_packages ctl-rehearsal.yaml "${SHIPPED[@]}"
	ctl ctl.yaml apply -f apps/today/dev/calendar.yaml >/dev/null
	assistant_off
	echo "today dev: restarted, and the packages added again"
	;;
reset)
	mise run dev:wipe
	mkdir -p .dev
	authority
	mise run dev:up
	setup
	;;
stop)
	mise run dev:stop
	;;
*)
	echo "usage: $0 up|restart|reset|stop" >&2
	exit 2
	;;
esac
