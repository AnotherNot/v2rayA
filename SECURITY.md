# Security policy

v2rayA runs as root on the host, manages the TUN device, routes, iptables/nftables
and DNS. The administrator interface is therefore equivalent to root: a stolen
session, a cross-site request or an XSS is not a proxy setting, it is code
execution on the machine.

## Supported versions

Security fixes land on the current release line of this fork (the `public`
branch). Older tags do not receive separate patches; upgrade to the latest
release.

## Reporting a vulnerability

Report it privately through GitHub's **Report a vulnerability** button on the
repository's Security tab (private vulnerability reporting is enabled):

<https://github.com/AnotherNot/v2rayA/security/advisories/new>

Do not open a public issue for a vulnerability that is not already public.
Please include:

- the affected component (`service/`, `core/`, `gui/`, `install/`) and version;
- a minimal reproduction, request, or configuration;
- the impact you observed and the conditions it needs (root, LAN, Docker,
  first run, an authenticated session);
- logs with tokens, subscription URLs, UUIDs and passwords redacted.

The maintainer will acknowledge the report, confirm the affected versions and
coordinate a fix and disclosure date. This is a small project without a
dedicated security team; the response is best effort.

## Scope

In scope: the service API and its authentication, the generated core
configuration, the DNS module, the GUI client, the install and container
scripts, and the release workflow.

Out of scope: a deployment that deliberately exposes the API without TLS or a
reverse proxy, the administrator's own TUN setup scripts (they run as root by
design and are acknowledged in the UI), and vulnerabilities in upstream
dependencies that have no reachable call path in this project.

## Accepted risks

- An administrator session is root-equivalent. TUN setup/teardown scripts run
  as root and are stored in the configuration database.
- The service runs as root in the container and needs `CAP_NET_ADMIN`,
  `CAP_NET_RAW` and `/dev/net/tun` for the transparent proxy.
- Subscriptions and rule sources are fetched from addresses the administrator
  supplies; private addresses are refused unless `--allow-private-fetch` is
  given.
