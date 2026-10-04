#!/usr/bin/env bash
set -uo pipefail

readonly agent_mail="${AGENT_MAIL_BIN:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/agent-mail}"
passed=0
failed=0

as() {
  local agent="$1"
  shift
  AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF="$agent" "$agent_mail" "$@"
}

expect() {
  local description="$1" expected_status="$2" expected_text="$3"
  shift 3
  local output status
  output="$("$@" 2>&1)"
  status=$?
  if [ "$status" = "$expected_status" ] && grep -q -- "$expected_text" <<<"$output"; then
    passed=$(( passed + 1 ))
    echo "ok   $description"
  else
    failed=$(( failed + 1 ))
    echo "FAIL $description (status $status, wanted $expected_status and '$expected_text')"
    sed 's/^/     | /' <<<"$output"
  fi
}

expect_absent() {
  local description="$1" unwanted_text="$2"
  shift 2
  if "$@" 2>&1 | grep -q -- "$unwanted_text"; then
    failed=$(( failed + 1 ))
    echo "FAIL $description ('$unwanted_text' still present)"
  else
    passed=$(( passed + 1 ))
    echo "ok   $description"
  fi
}

inbox_count() {
  local count=0 message
  for message in "$mailbox/to-$1"/*.md; do
    [ -e "$message" ] && count=$(( count + 1 ))
  done
  echo "$count"
}

fresh_mailbox() {
  mailbox="$(mktemp -d)/mail"
  as nobody init "$@" >/dev/null
  as alpha join alpha planner >/dev/null 2>&1
  as beta join beta builder >/dev/null 2>&1
  as gamma join gamma reviewer >/dev/null 2>&1
}

test_creating_a_mailbox() {
  mailbox="$(mktemp -d)/mail"
  expect "check before init reports no mailbox" 2 "DEAD: no mailbox" as alpha check
  expect "init creates the mailbox" 0 "created mailbox" as alpha init
  expect "a second init is refused" 1 "already exists" as alpha init
  expect "check without a deadline says none" 0 "DEADLINE: none" as alpha check
}

test_joining_and_names() {
  fresh_mailbox
  expect "who lists every member" 0 "gamma" as alpha who
  expect "a fresh name cannot be taken twice" 1 "is taken" as delta join alpha
  expect "a stale name can be taken over" 0 "taking over" env AGENT_MAIL_STALE_MINUTES=0 AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" "$agent_mail" join alpha
  expect "names are validated" 1 "lowercase" as x join "Bad Name"
  expect "commands need a joined identity" 1 "is not a member" as delta inbox
}

test_two_agents_on_one_machine() {
  fresh_mailbox
  printf 'hello beta\n' | as alpha send beta greeting >/dev/null
  expect "beta sees the message" 0 "from alpha" as beta inbox
  expect "alpha sees it as unread by beta" 0 "to-beta/" as alpha inbox
  [ "$(inbox_count alpha)" = 0 ] && passed=$(( passed + 1 )) && echo "ok   alpha's own inbox stays empty" \
    || { failed=$(( failed + 1 )); echo "FAIL alpha's own inbox stays empty"; }
}

test_fan_out_to_a_list() {
  fresh_mailbox
  expect "a list of members is set" 0 "@team: alpha beta gamma" as alpha list set team alpha beta gamma
  expect "a list with a non-member is refused" 1 "not members: zeta" as alpha list set bad alpha zeta
  expect "sending to the list skips the sender" 0 "to: beta gamma" as alpha send @team kickoff <<<"plan for today"
  expect "the copies share one id" 0 "^1$" bash -c "cat '$mailbox'/to-beta/*.md '$mailbox'/to-gamma/*.md | grep '^id:' | sort -u | wc -l | tr -d ' '"
  local message
  message="$(basename "$(ls "$mailbox"/to-beta/*.md)")"
  as beta ack "$message" >/dev/null
  expect "beta's ack leaves gamma's copy unread" 0 "to-gamma/$message" as alpha inbox
  expect "beta's ack is reflected for beta" 0 "unread in to-beta:" as beta inbox
  [ "$(inbox_count beta)" = 0 ] && [ "$(inbox_count gamma)" = 1 ] && passed=$(( passed + 1 )) && echo "ok   each recipient acks on its own" \
    || { failed=$(( failed + 1 )); echo "FAIL each recipient acks on its own"; }
}

test_replies_and_addressing() {
  fresh_mailbox
  printf 'question\n' | as alpha send beta,gamma ask >/dev/null
  local id
  id="$(sed -n 's/^id: //p' "$mailbox"/to-beta/*.md)"
  expect "a reply carries in-reply-to" 0 "re $id" bash -c "printf 'answer\n' | AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR='$mailbox' AGENT_MAIL_SELF=beta '$agent_mail' send --reply-to '$id' alpha answer >/dev/null && AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR='$mailbox' AGENT_MAIL_SELF=alpha '$agent_mail' inbox"
  expect "an unknown recipient refuses the whole send" 1 "unknown recipients: zeta" as alpha send beta,zeta hi <<<"hi"
  [ "$(inbox_count beta)" = 1 ] && passed=$(( passed + 1 )) && echo "ok   a refused send writes nothing" \
    || { failed=$(( failed + 1 )); echo "FAIL a refused send writes nothing"; }
  expect "a body with a secret is refused" 1 "carries a secret" as alpha send beta leak <<<"token: abcdef123456"
  expect "an empty body is refused" 1 "empty message" as alpha send beta empty </dev/null
}

test_a_dead_mailbox() {
  fresh_mailbox --deadline "2001-01-01 00:00"
  expect "check prints the deadline then DEAD" 2 "DEADLINE: 2001-01-01 00:00 UTC" as alpha check
  expect "check names the passed deadline" 2 "DEAD: the deadline passed" as alpha check
  expect "send is refused after the deadline" 2 "DEAD:" as alpha send beta late <<<"too late"
  expect "watch exits with DEAD" 2 "DEAD:" as alpha watch
  expect "an invalid deadline is refused at init" 1 "UTC" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$(mktemp -d)/m" "$agent_mail" init --deadline "next week"
}

test_the_sync_check() {
  fresh_mailbox --deadline "2099-01-01 00:00"
  expect "a live deadline shows time left" 0 "time left" as alpha check
  expect "a passing sync check keeps it live" 0 "time left" env AGENT_MAIL_SYNC_CHECK=true AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha "$agent_mail" check
  expect "a failing sync check makes it DEAD" 2 "DEAD: the sync check failed" env AGENT_MAIL_SYNC_CHECK=false AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha "$agent_mail" check
}

start_watch() {
  local agent="$1" seconds="$2"
  watch_log="$(mktemp)"
  AGENT_MAIL_POLL_SECONDS=1 AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF="$agent" \
    "$agent_mail" watch > "$watch_log" 2>&1 &
  watch_pid=$!
  ( sleep "$seconds"; kill "$watch_pid" 2>/dev/null ) &
  watch_timer_pid=$!
}

finish_watch() {
  wait "$watch_pid"
  watch_status=$?
  kill "$watch_timer_pid" 2>/dev/null
  wait "$watch_timer_pid" 2>/dev/null
}

watch_result() {
  cat "$watch_log"
  return "$watch_status"
}

test_watching_the_inbox() {
  fresh_mailbox
  printf 'already here\n' | as alpha send beta before-watch >/dev/null
  start_watch beta 6
  sleep 2
  printf 'first\n' | as alpha send beta during-watch-one >/dev/null
  sleep 2
  printf 'second\n' | as gamma send beta during-watch-two >/dev/null
  finish_watch
  expect "watch announces mail that arrives" 0 "NEW MAIL: .*during-watch-one.md from alpha" cat "$watch_log"
  expect "watch announces every arrival" 0 "NEW MAIL: .*during-watch-two.md from gamma" cat "$watch_log"
  expect_absent "watch stays quiet about mail that was already there" "before-watch" cat "$watch_log"
}

test_watch_ends_when_the_inbox_disappears() {
  fresh_mailbox
  start_watch gamma 6
  sleep 1
  rm -r "$mailbox/to-gamma"
  finish_watch
  expect "watch exits with DEAD when the inbox disappears" 2 "DEAD: the inbox" watch_result
}

test_untrusted_input_is_refused() {
  fresh_mailbox
  expect "a recipient path cannot leave the mailbox" 1 "unknown recipients" as alpha send "beta/../../../tmp" escape <<<"hi"
  expect "a list name cannot be a path" 1 "usage: agent-mail list delete" as alpha list delete ../members/beta
  [ -f "$mailbox/members/beta" ] && passed=$(( passed + 1 )) && echo "ok   a member survives a path-shaped list delete" \
    || { failed=$(( failed + 1 )); echo "FAIL a member survives a path-shaped list delete"; }
  expect "a role cannot inject header lines" 1 "one line" as delta join delta "$(printf 'x\nseen: 99999999999')"
  expect "a reply-to cannot inject header lines" 1 "reply-to takes a message id" as alpha send --reply-to "$(printf 'x\nfrom: mallory')" beta forged <<<"hi"
  expect "ack refuses names that are not messages" 1 "usage: agent-mail ack" as beta ack ..
  expect "a body over 1 MiB is refused" 1 "over 1048576 bytes" bash -c "head -c 1048600 /dev/zero | tr '\\0' 'a' | AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR='$mailbox' AGENT_MAIL_SELF=alpha '$agent_mail' send beta big"
  printf -- '---\nid: x\nfrom: \033[31mred\n---\nhi\n' > "$mailbox/to-beta/20990101T000000Z-forged.md"
  expect "inbox does not echo control characters from a sender field" 0 "from (unknown sender)" as beta inbox
  expect "an invalid AGENT_MAIL_SELF is refused" 1 "not a valid name" as "../members/beta" inbox
}

test_config_mistakes_are_refused() {
  fresh_mailbox
  local config_file
  config_file="$(mktemp)"
  printf 'AGENT_MAIL_DIRR=%s\n' "$mailbox" > "$config_file"
  expect "an unknown setting in the config file is refused" 1 "unknown setting 'AGENT_MAIL_DIRR'" env AGENT_MAIL_CONFIG="$config_file" AGENT_MAIL_DIR="$mailbox" "$agent_mail" check
  expect "a relative AGENT_MAIL_DIR is refused" 1 "absolute path" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR=relative/mail "$agent_mail" check
}

test_leaving_ends_a_running_watch() {
  fresh_mailbox
  start_watch beta 6
  sleep 1
  rm "$mailbox/members/beta"
  finish_watch
  expect "watch stops once its member has left" 1 "no longer a member" watch_result
}

readonly test_tools="$(mktemp -d)"
readonly fake_syncthing="$test_tools/fake-syncthing$(go env GOEXE)"
if ! (cd "$(dirname "${BASH_SOURCE[0]}")/.." && go build -o "$fake_syncthing" ./tests/fakesyncthing); then
  echo "FAIL building the fake syncthing with go build"
  exit 1
fi
readonly this_device="AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH"
readonly peer_device="ZZZZZZZ-YYYYYYY-XXXXXXX-WWWWWWW-VVVVVVV-UUUUUUU-TTTTTTT-SSSSSSS"

with_syncthing() {
  AGENT_MAIL_SYNCTHING="$fake_syncthing" FAKE_SYNCTHING_STATE="$syncthing_state" "$@"
}

fresh_syncthing() {
  syncthing_state="$(mktemp -d)"
  printf '%s\n' "$this_device" > "$syncthing_state/id"
}

test_doctor_explains_what_is_missing() {
  mailbox="$(mktemp -d)/mail"
  expect "doctor reports a missing mailbox and exits 1" 1 "problem  no mailbox at" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SYNCTHING=/nonexistent "$agent_mail" doctor
  fresh_mailbox
  expect "doctor accepts a local mailbox with warnings only" 0 "warning  no sync check and no Syncthing" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha AGENT_MAIL_SYNCTHING=/nonexistent "$agent_mail" doctor
  expect "doctor says how to install Syncthing" 0 "install Syncthing" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha AGENT_MAIL_SYNCTHING=/nonexistent "$agent_mail" doctor
  expect "doctor flags an agent without identity" 1 "this agent has no identity" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SYNCTHING=/nonexistent "$agent_mail" doctor
  expect "doctor changes nothing" 0 "nothing was changed" env AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha AGENT_MAIL_SYNCTHING=/nonexistent "$agent_mail" doctor
}

test_syncthing_sharing() {
  fresh_mailbox
  fresh_syncthing
  expect "sync id prints this device's ID" 0 "$this_device" with_syncthing as alpha sync id
  expect "doctor flags an unshared mailbox" 1 "does not share" with_syncthing as alpha doctor
  expect "share without --yes only prints the plan" 0 "will share folder 'agent-mail' with $peer_device" with_syncthing as alpha sync share "$peer_device" --address tcp://192.0.2.7:22000
  [ ! -e "$syncthing_state/shared" ] && [ ! -e "$mailbox/.stignore" ] && passed=$(( passed + 1 )) && echo "ok   a plan without --yes changes nothing" \
    || { failed=$(( failed + 1 )); echo "FAIL a plan without --yes changes nothing"; }
  expect "share --yes applies every step" 0 "done: add '\*.tmp'" with_syncthing as alpha sync share "$peer_device" --address tcp://192.0.2.7:22000 --yes
  expect "share is idempotent" 0 "nothing to do" with_syncthing as alpha sync share "$peer_device" --yes
  expect "status shows the peer" 0 "shared with: $peer_device" with_syncthing as alpha sync status
  expect "the built-in sync check passes once shared" 0 "time left\|DEADLINE: none" env AGENT_MAIL_SYNC_CHECK=syncthing AGENT_MAIL_SYNCTHING="$fake_syncthing" FAKE_SYNCTHING_STATE="$syncthing_state" AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha "$agent_mail" check
  touch "$syncthing_state/stopped"
  expect "the built-in sync check fails when Syncthing stops" 2 "DEAD: the sync check failed: Syncthing is not running" env AGENT_MAIL_SYNC_CHECK=syncthing AGENT_MAIL_SYNCTHING="$fake_syncthing" FAKE_SYNCTHING_STATE="$syncthing_state" AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha "$agent_mail" check
  expect "doctor gives the start command when Syncthing stops" 1 "start it" with_syncthing as alpha doctor
  rm "$syncthing_state/stopped"
  expect "share refuses a malformed device ID" 1 "a device id is 8 groups" with_syncthing as alpha sync share NOT-AN-ID
  expect "share refuses this device's own ID" 1 "this device's own ID" with_syncthing as alpha sync share "$this_device"
  expect "share refuses a malformed address" 1 "--address is" with_syncthing as alpha sync share "$peer_device" --address "192.0.2.7; rm -rf /"
  echo true > "$syncthing_state/option-relays-enabled"
  expect "doctor warns when traffic can leave the LAN" 0 "may reach beyond the LAN: relays" with_syncthing as alpha doctor

  fresh_mailbox
  fresh_syncthing
  printf 'keep-me' > "$mailbox/.stignore"
  with_syncthing as alpha sync share "$peer_device" --yes >/dev/null
  expect "share keeps an existing last .stignore line intact" 0 "^keep-me$" cat "$mailbox/.stignore"
  expect "share puts *.tmp on its own line" 0 "^\*\.tmp$" cat "$mailbox/.stignore"
}

test_leaving() {
  fresh_mailbox
  expect "leave drops the membership" 0 "left" as gamma leave
  expect_absent "who no longer lists the member" "gamma" as alpha who
}

for test_case in $(declare -F | awk '{print $3}' | grep '^test_'); do
  "$test_case"
done
echo "passed $passed, failed $failed"
[ "$failed" = 0 ]
