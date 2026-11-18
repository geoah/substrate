#!/usr/bin/env bash
#
# The push relay a release names (SUBSTRATE_APPLE_PUSH_RELAY), held to the
# rules every other side holds it to: `pushwire.Endpoint` in Go and
# `PushRelay.endpoint` in the kit, whose vectors are
# internal/pushwire/testdata/endpoints.json. Sourced by .mise/apps.sh, and
# run over those vectors by `lint:apps`, so a value the release stamps and
# publishes is one the app and the server take.

# push_relay_valid <relay>: 0 where it is an https URL with a host and a
# port, if any, from 1 to 65535, an optional path, and no user info, query
# or fragment, of printable ASCII and at most 2,048 characters, each `%`
# starting two hex digits (and, in the host, a byte past ASCII, so no IPv6
# zone: `%25` names an interface of one machine, which no relay needs).
push_relay_valid() {
	local LC_ALL=C relay="${1-}" rest authority host port="" tail
	[ -n "$relay" ] && [ "${#relay}" -le 2048 ] || return 1
	case "$relay" in https://*) ;; *) return 1 ;; esac
	case "$relay" in *[!!-~]* | *\?* | *#* | *\\*) return 1 ;; esac
	rest="${relay#https://}"
	authority="${rest%%/*}"
	[ -n "$authority" ] || return 1
	case "$authority" in *@*) return 1 ;; esac
	tail="$relay"
	while [[ $tail == *%* ]]; do
		tail="${tail#*%}"
		[[ $tail =~ ^[0-9A-Fa-f][0-9A-Fa-f] ]] || return 1
		tail="${tail:2}"
	done
	tail="$authority"
	while [[ $tail == *%* ]]; do
		tail="${tail#*%}"
		[[ $tail =~ ^[89A-Fa-f] ]] || return 1
		tail="${tail:2}"
	done
	if [ "${authority#\[}" != "$authority" ]; then
		case "$authority" in *]*) ;; *) return 1 ;; esac
		host="${authority%%]*}"
		host="${host#\[}"
		rest="${authority#*]}"
		case "$rest" in
		"") ;;
		:*) port="${rest#:}" && [ -n "$port" ] || return 1 ;;
		*) return 1 ;;
		esac
	else
		host="${authority%%:*}"
		case "$authority" in *:*) port="${authority#*:}" && [ -n "$port" ] || return 1 ;; esac
	fi
	[ -n "$host" ] || return 1
	if [ -n "$port" ]; then
		case "$port" in *[!0-9]*) return 1 ;; esac
		# Leading zeros are read past, as Go reads the number: :000443 is 443.
		port="${port#"${port%%[!0]*}"}"
		[ -n "$port" ] && [ "${#port}" -le 5 ] && [ "$((10#$port))" -le 65535 ] || return 1
	fi
	return 0
}
