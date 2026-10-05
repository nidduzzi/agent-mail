#!/usr/bin/env bash
set -uo pipefail

agent_mail="${AGENT_MAIL_BIN:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/agent-mail}"
syncthing="${SYNCTHING:-$(command -v syncthing)}"
strelaysrv="${STRELAYSRV:-$(command -v strelaysrv)}"
work="$(mktemp -d)"
pids=()
passed=0
failed=0

finish() {
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null
  done
  wait 2>/dev/null
  if [ "$failed" != 0 ]; then
    tail -n 20 "$work"/*.log
  fi
  if [ -n "${KEEP_E2E_LOGS:-}" ]; then
    cp "$work"/*.log "$KEEP_E2E_LOGS"/
  fi
  rm -rf "$work"
}
trap finish EXIT

free_port() {
  python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
}

as_side() {
  local side="$1" self="$2"
  shift 2
  AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$work/$side-mail" AGENT_MAIL_SELF="$self" \
    AGENT_MAIL_SYNCTHING="$syncthing" AGENT_MAIL_SYNCTHING_HOME="$work/$side" "$agent_mail" "$@"
}

eventually() {
  local description="$1" seconds="$2" expected_text="$3"
  shift 3
  local output=""
  for _ in $(seq "$seconds"); do
    output="$("$@" 2>&1)"
    if grep -q -- "$expected_text" <<<"$output"; then
      passed=$(( passed + 1 ))
      echo "ok   $description"
      return 0
    fi
    sleep 1
  done
  failed=$(( failed + 1 ))
  echo "FAIL $description (no '$expected_text' within ${seconds}s)"
  sed 's/^/     | /' <<<"$output"
  return 1
}

never_contacts_public_servers() {
  if grep -iE 'https?://[^ "]*syncthing\.net' "$work"/*.log >/dev/null; then
    failed=$(( failed + 1 ))
    echo "FAIL nothing contacts Syncthing's public servers"
    grep -iE 'https?://[^ "]*syncthing\.net' "$work"/*.log | head -5 | sed 's/^/     | /'
  else
    passed=$(( passed + 1 ))
    echo "ok   nothing contacts Syncthing's public servers"
  fi
}

start_relay() {
  mkdir -p "$work/relay"
  "$strelaysrv" -keys "$work/relay" -listen "127.0.0.1:$relay_port" -pools "" -status-srv "" -protocol tcp4 -nat=false >>"$work/relay.log" 2>&1 &
  relay_pid=$!
  pids+=("$relay_pid")
}

relay_port="$(free_port)"
start_relay
eventually "the private relay starts" 30 "URI: relay://" cat "$work/relay.log" || exit 1
relay_uri="$(sed -n 's/.*URI: \(relay:[^ ]*\).*/\1/p' "$work/relay.log" | head -1)"

for side in a b; do
  "$syncthing" generate --home="$work/$side" --no-port-probing >"$work/$side-generate.log" 2>&1
  gui_port="$(free_port)"
  sed -i \
    -e "s#<listenAddress>default</listenAddress>#<listenAddress>${relay_uri//&/\\&amp;}</listenAddress>#" \
    -e 's#<globalAnnounceEnabled>true#<globalAnnounceEnabled>false#' \
    -e 's#<localAnnounceEnabled>true#<localAnnounceEnabled>false#' \
    -e 's#<natEnabled>true#<natEnabled>false#' \
    -e 's#<startBrowser>true#<startBrowser>false#' \
    -e 's#<urAccepted>0#<urAccepted>-1#' \
    -e 's#<autoUpgradeIntervalH>12#<autoUpgradeIntervalH>0#' \
    -e 's#<crashReportingEnabled>true#<crashReportingEnabled>false#' \
    -e 's#<stunServer>default</stunServer>#<stunServer>127.0.0.1:9</stunServer>#' \
    -e 's#<reconnectionIntervalS>[0-9]*#<reconnectionIntervalS>2#' \
    -e 's#<relayReconnectIntervalM>[0-9]*#<relayReconnectIntervalM>1#' \
    -e "s#<address>127.0.0.1:8384</address>#<address>127.0.0.1:$gui_port</address>#" \
    "$work/$side/config.xml"
  mkdir -p "$work/$side-mail"
  STNODEFAULTFOLDER=1 STNOUPGRADE=1 "$syncthing" serve --home="$work/$side" --no-browser --no-upgrade --no-restart >"$work/$side.log" 2>&1 &
  pids+=("$!")
done

for side in a b; do
  eventually "Syncthing $side answers its CLI" 30 "myID" "$syncthing" cli --home="$work/$side" show system || exit 1
done

as_side a "" init --deadline "2099-01-01 00:00" >/dev/null
id_a="$(as_side a "" sync id)"
id_b="$(as_side b "" sync id)"
as_side a "" sync share "$id_b" --address "$relay_uri" --yes >/dev/null
as_side b "" sync share "$id_a" --address "$relay_uri" --yes >/dev/null

eventually "a reaches b through the private relay" 60 "connected via relay 127.0.0.1:$relay_port" as_side a "" sync status
eventually "b reaches a through the private relay" 30 "connected via relay 127.0.0.1:$relay_port" as_side b "" sync status
eventually "the mailbox syncs to b" 60 "AGENT-MAIL DEADLINE: 2099-01-01" as_side b "" check

as_side a alpha join alpha >/dev/null
as_side b beta join beta >/dev/null
eventually "each side sees both members" 60 "beta" as_side a alpha who
printf 'hello over the relay\n' | as_side a alpha send beta hello >/dev/null
eventually "mail from a arrives at b" 60 "from alpha" as_side b beta inbox
message="$(ls "$work/b-mail/to-beta" | grep '\.md$' | head -1)"
as_side b beta ack "$message" >/dev/null
eventually "the ack reaches the sender" 60 "not yet read:$" bash -c "AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR='$work/a-mail' AGENT_MAIL_SELF=alpha '$agent_mail' inbox | tail -1"
eventually "the built-in sync check passes" 10 "time left" env AGENT_MAIL_SYNC_CHECK=syncthing bash -c "AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR='$work/a-mail' AGENT_MAIL_SELF=alpha AGENT_MAIL_SYNCTHING='$syncthing' AGENT_MAIL_SYNCTHING_HOME='$work/a' '$agent_mail' check"
eventually "doctor reports the relayed peer" 10 "ok       peer $id_b connected via relay" as_side a alpha doctor

kill "$relay_pid"
wait "$relay_pid" 2>/dev/null
eventually "losing the relay disconnects the peers" 60 "not connected" as_side a alpha sync status
eventually "doctor warns while nobody is connected" 10 "no peer is connected yet" as_side a alpha doctor
start_relay
eventually "the peers reconnect when the relay returns" 240 "connected via relay 127.0.0.1:$relay_port" as_side a alpha sync status

never_contacts_public_servers
echo "passed $passed, failed $failed"
[ "$failed" = 0 ]
