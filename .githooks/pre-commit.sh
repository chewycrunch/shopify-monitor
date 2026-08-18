#!/bin/sh
# Blocks a commit that is unformatted, not lint-clean, or whose go.mod/go.sum
# have drifted from the source.
#
# Install with `task hooks`, which points core.hooksPath at .githooks/.
# The same three checks run as `task check`; bypass once with
# `git commit --no-verify`.
#
# The checks read the working tree, not the staged snapshot. A staged fix whose
# file is still dirty gets judged on the dirty version, so commit from a clean
# tree if you want the result to mean anything.

if git diff --cached --quiet -- '*.go' go.mod go.sum; then
    exit 0
fi

command -v go >/dev/null || {
    echo "pre-commit: go not found on PATH" >&2
    exit 1
}

# Resolved the way the Taskfile resolves it: `task tools` installs into
# GOPATH/bin, which is not necessarily on PATH.
golangci="$(go env GOPATH)/bin/golangci-lint"
if [ ! -x "$golangci" ]; then
    golangci=$(command -v golangci-lint) || {
        echo "pre-commit: golangci-lint not found — install it with 'task tools'" >&2
        exit 1
    }
fi

failed=''

# gofmt alone would not be enough: .golangci.yml also enables goimports with a
# local prefix, and import grouping is invisible to gofmt.
"$golangci" fmt --diff ./... || failed="$failed  task fmt\n"

go mod tidy -diff || failed="$failed  go mod tidy\n"

"$golangci" run ./... || failed="$failed  task lint\n"

[ -z "$failed" ] && exit 0

echo >&2
echo "pre-commit failed. Fix with:" >&2
printf '%b' "$failed" >&2
echo "  git commit --no-verify   # to bypass" >&2
exit 1
