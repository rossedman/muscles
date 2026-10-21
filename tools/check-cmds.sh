#!/usr/bin/env sh
# Fails if a file in internal/cli defines more than one command. A command
# is a &cobra.Command{} literal or a noun() call; helper files (app.go,
# usage.go, ...) define none, and noun() itself is the factory in root.go.
fail=0
for f in internal/cli/*.go; do
  case "$f" in *_test.go) continue ;; esac
  lit=$(grep -c '&cobra.Command{' "$f")
  calls=$(grep -v '^func noun(' "$f" | grep -c '[^a-zA-Z_.]noun(')
  case "$f" in internal/cli/root.go) lit=$((lit - 1)) ;; esac # the noun() factory
  n=$((lit + calls))
  if [ "$n" -gt 1 ]; then
    echo "$f defines $n commands; put each in its own file"
    fail=1
  fi
done
exit $fail
