#!/usr/bin/env bash
#
# Apply the repository settings SEC-15 asks for. This is an owner action: it
# needs a token with administration rights and it changes the repository, not
# this checkout. Review the calls before running them.
#
# Usage:
#   REPO=AnotherNot/v2rayA ./install/security/github-hardening.sh
#   REVIEWER_ID=123456 REPO=... ./install/security/github-hardening.sh
#
# REVIEWER_ID is the numeric GitHub user id of the release reviewer. Without
# it the environments are created without protection; add the reviewer in
# Settings -> Environments afterwards.

set -euo pipefail

repo="${REPO:-AnotherNot/v2rayA}"
branch="${BRANCH:-public}"

echo "repository: $repo"
echo "protected branch: $branch"

api() {
  gh api -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "$@"
}

# Classic branch protection: a pull request with a review, no force-push,
# no deletion, and admins held to it as well.
api -X PUT "repos/$repo/branches/$branch/protection" --input - <<'JSON'
{
  "required_status_checks": null,
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
JSON

# Require full commit SHAs for every action, but keep the action allowlist
# open so Dependabot can still bump a pinned action.
api -X PUT "repos/$repo/actions/permissions" \
  -F enabled=true \
  -f allowed_actions=all \
  -F sha_pinning_required=true

# Secret scanning and push protection.
api -X PATCH "repos/$repo" -f 'security_and_analysis[secret_scanning][status]=enabled' \
  -f 'security_and_analysis[secret_scanning_push_protection][status]=enabled' \
  -f 'security_and_analysis[secret_scanning_non_provider_patterns][status]=enabled' \
  -f 'security_and_analysis[secret_scanning_validity_checks][status]=enabled'

# Dependabot alerts and security updates.
api -X PUT "repos/$repo/vulnerability-alerts"
api -X PUT "repos/$repo/automated-security-fixes"

# Private vulnerability reporting: researchers get a channel that is not a
# public issue.
api -X PUT "repos/$repo/private-vulnerability-reporting" || true

# Immutable releases: a published asset cannot be swapped under the same tag.
api -X PUT "repos/$repo/immutable-releases" || true

# Default code scanning (CodeQL) for the languages GitHub can configure
# without a custom build. Do not add an advanced codeql.yml while default
# setup is enabled: GitHub rejects SARIF from an advanced configuration.
api -X PATCH "repos/$repo/code-scanning/default-setup" \
  -f state=configured \
  -f 'query_suite=default' || true

# Release environments. Secrets stay repo-wide; these environments exist so
# a reviewer can be required before any release job starts.
for env in release aur; do
  if [[ -n "${REVIEWER_ID:-}" ]]; then
    api -X PUT "repos/$repo/environments/$env" --input - <<JSON
{"reviewers":[{"type":"User","id":$REVIEWER_ID}]}
JSON
  else
    api -X PUT "repos/$repo/environments/$env" --input - <<'JSON'
{}
JSON
  fi
  echo "environment ready: $env"
done

echo
echo "Applied. Verify with:"
echo "  gh api repos/$repo/branches/$branch/protection"
echo "  gh api repos/$repo/actions/permissions"
echo "  gh api repos/$repo | jq .security_and_analysis"
echo "Add required reviewers in Settings -> Environments if REVIEWER_ID was unset."
