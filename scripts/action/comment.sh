#!/usr/bin/env bash
# Post the action's report on the pull request, or update the comment it
# posted before, found by a hidden marker naming the image. Best effort: a
# token that can't comment (a fork's pull request) is a warning, not a failure.
set -uo pipefail

repo=$GITHUB_REPOSITORY
marker="<!-- muscles-action image=$IMAGE -->"
body="${RUNNER_TEMP:-/tmp}/muscles-comment.md"
{
  echo "$marker"
  # GitHub caps comments at 65536 characters.
  head -c 60000 "$REPORT"
  if [[ $(wc -c < "$REPORT") -gt 60000 ]]; then
    printf '\n\n_The report is cut short; the job summary has all of it._\n'
  fi
  # shellcheck disable=SC2016 # the backticks are Markdown
  printf '\n<sub>Posted by [muscles](https://github.com/rossedman/muscles) for `%s`. Updated on every push.</sub>\n' "$IMAGE"
} > "$body"

id=$(gh api --paginate "repos/$repo/issues/$PR/comments" --jq ".[] | select(.body | startswith(\"$marker\")) | .id" 2>/dev/null | head -1)
payload=$(jq -Rs '{body: .}' < "$body")
method=POST path="repos/$repo/issues/$PR/comments"
if [[ -n $id ]]; then
  method=PATCH path="repos/$repo/issues/comments/$id"
fi
if ! out=$(gh api -X "$method" "$path" --input - <<< "$payload" 2>&1 >/dev/null); then
  echo "::warning title=muscles::could not comment on the pull request (give the job pull-requests: write): $out"
fi
exit 0
