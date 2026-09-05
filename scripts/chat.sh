#!/bin/sh
# Opens the chat TUI against the local gateway, starting the gateway first if
# nothing is answering on its port.
#
# Usage: scripts/chat.sh [profile]
#
# With a profile the gateway is asked to load it up front, which is worth doing
# when the model is large and the first message would otherwise wait on it.
# Without one the gateway starts empty and loads whatever the first message
# names. OUTRIDER_PORT applies here the same way it does everywhere else.
set -eu

PORT="${OUTRIDER_PORT:-11435}"
ENDPOINT="http://127.0.0.1:$PORT"
READY_TRIES=30

fail() {
	echo "outrider: $1" >&2
	exit 1
}

gateway_ready() {
	curl -fsS --max-time 2 "$ENDPOINT/health" >/dev/null 2>&1
}

wait_ready() {
	tries=0
	while [ "$tries" -lt "$READY_TRIES" ]; do
		if gateway_ready; then
			return 0
		fi
		tries=$((tries + 1))
		sleep 1
	done
	return 1
}

main() {
	command -v outrider >/dev/null 2>&1 || fail "outrider is not on PATH"
	command -v curl >/dev/null 2>&1 || fail "requires curl"

	if gateway_ready; then
		if [ "$#" -gt 0 ]; then
			outrider use "$1" >/dev/null || fail "could not load $1"
		fi
	else
		echo "starting the gateway on $ENDPOINT..." >&2
		if [ "$#" -gt 0 ]; then
			outrider serve "$1" >/dev/null || fail "could not start the gateway with $1"
		else
			outrider start >/dev/null || fail "could not start the gateway"
		fi
		wait_ready || fail "the gateway did not answer on $ENDPOINT"
	fi

	exec outrider chat --endpoint "$ENDPOINT"
}

main "$@"
