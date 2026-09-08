#!/usr/bin/env bash
#
# The shard partition: names on stdin, shard SHARD of SHARDS on stdout.
#
# The names are sorted (LC_ALL=C, so every machine sorts alike) and shard k
# takes every SHARDS-th name starting from the k-th. Every name lands in
# exactly one shard by construction and a new name lands in one without
# anybody editing a list. It is its own script so .mise/cicheck.sh can hold
# the partition to that promise on a fixed list, without compiling anything.
#
# Round robin over the sorted names rather than a hash of each: it puts the
# same count of names in every shard, and on the engine suite's recorded
# per-test timings it spreads the total more evenly than a hash does (the
# slowest tests are a few 30 second TOTP waits, and a hash was free to stack
# them).
#
# Exit 2 on a SHARD or SHARDS that is not a positive integer or out of range,
# exit 1 on a shard that would select nothing.
set -euo pipefail

shards="${SHARDS:-1}"
shard="${SHARD:-1}"
case "${shards}${shard}" in
*[!0-9]* | "")
  echo "shardselect: SHARD and SHARDS must be positive integers (got SHARD=${shard} SHARDS=${shards})" >&2
  exit 2
  ;;
esac
if [ "$shards" -lt 1 ] || [ "$shard" -lt 1 ] || [ "$shard" -gt "$shards" ]; then
  echo "shardselect: SHARD must be in 1..SHARDS (got SHARD=${shard} SHARDS=${shards})" >&2
  exit 2
fi

mine="$(LC_ALL=C sort | awk -v n="$shards" -v k="$shard" 'NF && NR % n == k % n')"
if [ -z "$mine" ]; then
  echo "shardselect: shard ${shard}/${shards} selects nothing; SHARDS is larger than the list" >&2
  exit 1
fi
printf '%s\n' "$mine"
