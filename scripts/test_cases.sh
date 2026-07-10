#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:18088}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

post_message() {
  local payload="$1"
  local expected_status="$2"
  local expected_json_filter="${3:-.}"

  local response
  response="$(curl -sS -w $'\n%{http_code}' \
    -X POST "$BASE_URL/messages" \
    -H 'Content-Type: application/json' \
    -d "$payload")"

  local status
  status="$(printf '%s' "$response" | tail -n1)"
  local body
  body="$(printf '%s' "$response" | sed '$d')"

  if [[ "$status" != "$expected_status" ]]; then
    echo "POST failed: expected HTTP $expected_status, got $status" >&2
    echo "$body" >&2
    exit 1
  fi

  if ! printf '%s' "$body" | jq -e "$expected_json_filter" >/dev/null; then
    echo "POST response assertion failed: $expected_json_filter" >&2
    echo "$body" >&2
    exit 1
  fi
}

post_message_allow_statuses() {
  local payload="$1"
  shift
  local allowed_statuses=("$@")

  local response
  response="$(curl -sS -w $'\n%{http_code}' \
    -X POST "$BASE_URL/messages" \
    -H 'Content-Type: application/json' \
    -d "$payload")"

  local status
  status="$(printf '%s' "$response" | tail -n1)"
  local body
  body="$(printf '%s' "$response" | sed '$d')"

  for allowed in "${allowed_statuses[@]}"; do
    if [[ "$status" == "$allowed" ]]; then
      return 0
    fi
  done

  echo "POST failed: expected one of ${allowed_statuses[*]}, got $status" >&2
  echo "$body" >&2
  return 1
}

post_concurrently() {
  local expected_statuses="$1"
  shift
  local pids=()
  local failures=0

  for payload in "$@"; do
    (
      # shellcheck disable=SC2206
      local statuses=($expected_statuses)
      post_message_allow_statuses "$payload" "${statuses[@]}"
    ) &
    pids+=("$!")
  done

  for pid in "${pids[@]}"; do
    if ! wait "$pid"; then
      failures=$((failures + 1))
    fi
  done

  if [[ "$failures" -ne 0 ]]; then
    echo "$failures concurrent POST(s) failed" >&2
    exit 1
  fi
}

get_json() {
  local path="$1"
  local expected_status="$2"
  local expected_json_filter="$3"

  local response
  response="$(curl -sS -w $'\n%{http_code}' "$BASE_URL$path")"

  local status
  status="$(printf '%s' "$response" | tail -n1)"
  local body
  body="$(printf '%s' "$response" | sed '$d')"

  if [[ "$status" != "$expected_status" ]]; then
    echo "GET $path failed: expected HTTP $expected_status, got $status" >&2
    echo "$body" >&2
    exit 1
  fi

  if ! printf '%s' "$body" | jq -e "$expected_json_filter" >/dev/null; then
    echo "GET $path assertion failed: $expected_json_filter" >&2
    echo "$body" >&2
    exit 1
  fi
}

message() {
  local channel="$1"
  local number="$2"
  local type="$3"
  local body="$4"
  local time="2022-02-02T19:39:$(printf '%02d' "$number").86337+01:00"

  jq -nc \
    --arg channel "$channel" \
    --argjson number "$number" \
    --arg messageTime "$time" \
    --arg messageType "$type" \
    --argjson message "$body" \
    '{
      metadata: {
        channel: $channel,
        messageNumber: $number,
        messageTime: $messageTime,
        messageType: $messageType
      },
      message: $message
    }'
}

echo "Checking health"
get_json "/healthz" "200" '.status == "ok" and .checks.sqlite == "ok"'

rocket="script-rocket-main"

echo "RocketLaunched"
launch="$(message "$rocket" 1 RocketLaunched '{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}')"
post_message "$launch" "201" '.data.status == "created" and .data.materialized == true'
get_json "/rockets/$rocket" "200" '.data.type == "Falcon-9" and .data.speed == 500 and .data.mission == "ARTEMIS" and .data.status == "launched"'

echo "Exact duplicate RocketLaunched"
post_message "$launch" "200" '.data.status == "duplicate" and .data.materialized == false'

echo "Conflicting duplicate is ignored as 2xx for runner compatibility"
conflicting_launch="$(message "$rocket" 1 RocketLaunched '{"type":"Falcon-9","launchSpeed":500,"mission":"GEMINI"}')"
post_message "$conflicting_launch" "200" '.data.status == "conflict_ignored" and .data.materialized == false'
get_json "/rockets/$rocket" "200" '.data.mission == "ARTEMIS"'

echo "RocketSpeedIncreased"
post_message "$(message "$rocket" 2 RocketSpeedIncreased '{"by":3000}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.speed == 3500'

echo "RocketSpeedDecreased"
post_message "$(message "$rocket" 3 RocketSpeedDecreased '{"by":2500}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.speed == 1000'

echo "RocketMissionChanged"
post_message "$(message "$rocket" 4 RocketMissionChanged '{"newMission":"SHUTTLE_MIR"}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.mission == "SHUTTLE_MIR"'

echo "RocketExploded"
post_message "$(message "$rocket" 5 RocketExploded '{"reason":"PRESSURE_VESSEL_FAILURE"}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.status == "exploded" and .data.explosionReason == "PRESSURE_VESSEL_FAILURE"'

echo "Later events after explosion still apply"
post_message "$(message "$rocket" 6 RocketSpeedIncreased '{"by":100}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.status == "exploded" and .data.speed == 1100'

echo "Negative speed is allowed"
post_message "$(message "$rocket" 7 RocketSpeedDecreased '{"by":2000}')" "201" '.data.status == "created"'
get_json "/rockets/$rocket" "200" '.data.speed == -900'

gap_rocket="script-rocket-gap"

echo "Out-of-order future event is stored but not visible without message 1"
post_message "$(message "$gap_rocket" 2 RocketSpeedIncreased '{"by":3000}')" "201" '.data.status == "created" and .data.materialized == false'
get_json "/rockets/$gap_rocket" "404" '.error.code == "not_found"'

echo "Message 1 materializes and applies contiguous message 2"
post_message "$(message "$gap_rocket" 1 RocketLaunched '{"type":"Atlas-H","launchSpeed":500,"mission":"APOLLO"}')" "201" '.data.status == "created" and .data.materialized == true'
get_json "/rockets/$gap_rocket" "200" '.data.speed == 3500 and .data.lastMessageNumber == 2 and .data.pendingEvents == 0'

pending_rocket="script-rocket-pending"

echo "Sequence gap leaves later events pending"
post_message "$(message "$pending_rocket" 1 RocketLaunched '{"type":"Scout","launchSpeed":500,"mission":"SKYLAB"}')" "201" '.data.status == "created"'
post_message "$(message "$pending_rocket" 4 RocketSpeedIncreased '{"by":100}')" "201" '.data.status == "created"'
post_message "$(message "$pending_rocket" 5 RocketMissionChanged '{"newMission":"GEMINI"}')" "201" '.data.status == "created"'
get_json "/rockets/$pending_rocket" "200" '.data.lastMessageNumber == 1 and .data.pendingEvents == 2 and .data.mission == "SKYLAB"'

echo "Filling the gap applies pending events"
post_message "$(message "$pending_rocket" 2 RocketSpeedIncreased '{"by":100}')" "201" '.data.status == "created"'
post_message "$(message "$pending_rocket" 3 RocketSpeedIncreased '{"by":100}')" "201" '.data.status == "created"'
get_json "/rockets/$pending_rocket" "200" '.data.lastMessageNumber == 5 and .data.pendingEvents == 0 and .data.speed == 800 and .data.mission == "GEMINI"'

concurrent_rocket="script-rocket-concurrent-ooo"

echo "Concurrent out-of-order delivery with duplicates"
concurrent_launch="$(message "$concurrent_rocket" 1 RocketLaunched '{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}')"
concurrent_speed_2="$(message "$concurrent_rocket" 2 RocketSpeedIncreased '{"by":3000}')"
concurrent_speed_3="$(message "$concurrent_rocket" 3 RocketSpeedDecreased '{"by":2500}')"

post_concurrently "200 201" \
  "$concurrent_speed_3" \
  "$concurrent_speed_2" \
  "$concurrent_speed_2" \
  "$concurrent_launch" \
  "$concurrent_launch"

get_json "/rockets/$concurrent_rocket" "200" '.data.lastMessageNumber == 3 and .data.pendingEvents == 0 and .data.speed == 1000 and .data.mission == "ARTEMIS"'

invalid_rocket="script-rocket-invalid"

echo "Message 1 not RocketLaunched stays invisible"
post_message "$(message "$invalid_rocket" 1 RocketSpeedIncreased '{"by":100}')" "201" '.data.status == "created" and .data.materialized == false'
get_json "/rockets/$invalid_rocket" "404" '.error.code == "not_found"'

echo "Validation errors"
post_message '{"bad":true}' "400" '.error.code == "validation_error"'
post_message "$(message script-rocket-invalid-payload 1 RocketSpeedIncreased '{"by":-1}')" "400" '.error.code == "validation_error"'

echo "List and sort"
get_json "/rockets?sort=channel&order=asc" "200" '.meta.count >= 3 and .meta.sort == "channel" and .meta.order == "asc"'
get_json "/rockets?sort=speed&order=desc" "200" '.meta.sort == "speed" and .meta.order == "desc"'
get_json "/rockets?sort=unknown" "400" '.error.code == "validation_error"'

echo "All scripted cases passed"
