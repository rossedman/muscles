#!/usr/bin/env bash
# Write the action's Markdown report: what changed since the baseline (when
# there is one) and what to fix first. It goes to the job summary and to a
# file the comment step posts. Also sets the size outputs.
set -euo pipefail

report="${RUNNER_TEMP:-/tmp}/muscles-report.md"
{
  if [[ -n ${BASELINE:-} ]]; then
    muscles images diff "$BASELINE" "$IMAGE" -o markdown
    echo
  fi
  muscles images inspect "$IMAGE" -o markdown
} > "$report"
cat "$report" >> "$GITHUB_STEP_SUMMARY"

json=$(muscles images inspect "$IMAGE" --json)
{
  echo "report=$report"
  echo "size=$(jq -r .image.size <<< "$json")"
  echo "waste=$(jq -r .image.waste <<< "$json")"
  if [[ -n ${BASELINE:-} ]]; then
    echo "size-delta=$(muscles images diff "$BASELINE" "$IMAGE" --json | jq -r .size_delta)"
  fi
} >> "$GITHUB_OUTPUT"
