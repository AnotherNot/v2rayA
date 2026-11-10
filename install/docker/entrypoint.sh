#!/bin/sh
# The service keeps accounts, the session secret and subscriptions under
# /etc/v2raya; a bind-mounted directory often arrives with the host umask
# (0755 or worse). Tighten it before the service opens the database.
set -eu
umask 0077
if [ "$(id -u)" = "0" ]; then
  mkdir -p /etc/v2raya
  chmod 0750 /etc/v2raya 2>/dev/null || true
fi
exec "$@"
