#!/usr/bin/env bash
# Run `muscles images check` for the action: annotations and the table in the
# log, the Markdown results added to the report, and with SARIF=true a SARIF
# log for code scanning instead of the annotations (they would say the same
# thing twice). Never fails the step itself, so the comment is still posted;
# the action's last step exits with the code.
set -uo pipefail

args=()
[[ -n ${BASELINE:-} ]] && args+=(--baseline "$BASELINE")
# shellcheck disable=SC2206 # ARGS is a list of flags, split on spaces
[[ -n ${ARGS:-} ]] && args+=($ARGS)

if [[ ${SARIF:-} == true ]]; then
  GITHUB_ACTIONS=false muscles images check "$IMAGE" "${args[@]}" -o table
else
  muscles images check "$IMAGE" "${args[@]}" -o table
fi
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

if [[ ${SARIF:-} == true ]]; then
  sarif="${RUNNER_TEMP:-/tmp}/muscles.sarif"
  muscles images check "$IMAGE" "${args[@]}" -o sarif --quiet > "$sarif" 2>/dev/null
  echo "sarif=$sarif" >> "$GITHUB_OUTPUT"
fi

ok=true
[[ $code == 0 ]] || ok=false
{
  echo "code=$code"
  echo "ok=$ok"
} >> "$GITHUB_OUTPUT"
