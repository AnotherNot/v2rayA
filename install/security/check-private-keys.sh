#!/usr/bin/env bash
#
# Refuse a checkout that carries a private key or a deployment credential.
# The history is not rewritten: SEC-16 handles the keys that were committed
# years ago by revoking them, and this gate stops a new one from arriving.
set -euo pipefail

status=0

forbidden=$(git ls-files | grep -E '(^|/)(deploy_key|private-key\.asc|ssh-add_expect|id_rsa|id_ed25519)$' || true)
if [[ -n "$forbidden" ]]; then
  echo "private-key material is tracked in the working tree:" >&2
  echo "$forbidden" >&2
  status=1
fi

if git grep -nI -E -e '-----BEGIN (RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----' -- \
  ':!docs/security/credential-rotation.md' \
  ':!install/security/check-private-keys.sh'; then
  echo "a tracked file contains a PEM private-key header" >&2
  status=1
fi

if [[ $status -eq 0 ]]; then
  echo "no private keys in the tracked tree"
fi
exit $status
