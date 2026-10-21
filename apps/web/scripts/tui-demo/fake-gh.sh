#!/bin/bash
# Fake `gh` for the TUI demo: canned pull requests and issues, no network.
case "$1 $2" in
  "pr list")
    cat <<'JSON'
[
 {"number":12,"title":"Billing: invoice webhooks","headRefName":"feat/billing","isCrossRepository":false,"isDraft":false,"state":"OPEN","updatedAt":"2026-10-20T10:00:00Z","url":"https://github.com/acme/api/pull/12","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]},
 {"number":14,"title":"Fix auth token timeout","headRefName":"fix/auth-timeout","isCrossRepository":false,"isDraft":false,"state":"OPEN","updatedAt":"2026-10-20T11:00:00Z","url":"https://github.com/acme/api/pull/14","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]},
 {"number":15,"title":"Bump dependencies","headRefName":"chore/deps","isCrossRepository":false,"isDraft":false,"state":"OPEN","updatedAt":"2026-10-20T12:00:00Z","url":"https://github.com/acme/api/pull/15","statusCheckRollup":[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}]},
 {"number":21,"title":"Rate limiting middleware","headRefName":"feat/rate-limit","isCrossRepository":false,"isDraft":false,"state":"OPEN","updatedAt":"2026-10-20T13:00:00Z","url":"https://github.com/acme/api/pull/21","statusCheckRollup":[]}
]
JSON
    ;;
  "issue list")
    cat <<'JSON'
[{"number":31,"title":"Crash when an invoice has no line items","labels":[{"name":"bug"}]},
 {"number":33,"title":"Add CSV export for usage reports","labels":[{"name":"enhancement"}]},
 {"number":36,"title":"Document the webhook retry policy","labels":[{"name":"documentation"}]}]
JSON
    ;;
  *) echo "fake gh: unsupported: $*" >&2; exit 1 ;;
esac
