#!/usr/bin/env bash
# Run `muscles images check` for the action: annotations and the table in the
# log, the Markdown results added to the report. Never fails the step itself,
# so the comment is still posted; the action's last step exits with the code.
set -uo pipefail

args=()
[[ -n ${BASELINE:-} ]] && args+=(--baseline "$BASELINE")
# shellcheck disable=SC2206 # ARGS is a list of flags, split on spaces
[[ -n ${ARGS:-} ]] && args+=($ARGS)

muscles images check "$IMAGE" "${args[@]}" -o table
code=$?
case $code in
  0 | 5) ;;
  *) echo "code=$code" >> "$GITHUB_OUTPUT"; echo "ok=false" >> "$GITHUB_OUTPUT"; exit 0 ;;
esac

{
  echo
  muscles images check "$IMAGE" "${args[@]}" -o markdown --quiet
} > "${RUNNER_TEMP:-/tmp}/muscles-check.md" 2>/dev/null
# Checks go first in the report: they decide whether the build passes.
if [[ -n ${REPORT:-} ]]; then
  cat "${RUNNER_TEMP:-/tmp}/muscles-check.md" "$REPORT" > "$REPORT.new" && mv "$REPORT.new" "$REPORT"
fi
cat "${RUNNER_TEMP:-/tmp}/muscles-check.md" >> "$GITHUB_STEP_SUMMARY"

ok=true
[[ $code == 0 ]] || ok=false
{
  echo "code=$code"
  echo "ok=$ok"
} >> "$GITHUB_OUTPUT"
