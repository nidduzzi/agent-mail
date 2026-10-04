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
  expect "commands need a joined identity" 1 "has not joined" as delta inbox
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

test_a_failing_sync_check() {
  fresh_mailbox --deadline "2099-01-01 00:00"
  expect "a live deadline shows time left" 0 "time left" as alpha check
  expect "a failing sync check makes it DEAD" 2 "DEAD: the sync check failed" env AGENT_MAIL_SYNC_CHECK=false AGENT_MAIL_CONFIG=/dev/null AGENT_MAIL_DIR="$mailbox" AGENT_MAIL_SELF=alpha "$agent_mail" check
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
