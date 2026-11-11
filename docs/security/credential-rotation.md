# Rotation of the credentials exposed in git history (SEC-16)

The upstream history carried private keys from 2021: an OpenSSH deploy key and
two GPG private keys. They left the tree long ago but stayed in every clone.

The history was rewritten when the repository moved to `AnotherNot/v2rayA`:
those blobs are not part of any commit here, and the previous repository was
deleted. That removes the keys from a fresh clone; it does not revoke them. A
key is only retired when its owner revokes it at the relying party, and every
copy made before the rewrite, including forks, caches and archives, still
contains it. The checklist below is an owner action: only the AUR account
holder and the Debian/GPG key holder can complete it.

## What had to be removed

| File | Kind |
| --- | --- |
| `install/deploy_key`, later `install/aur/deploy_key` | OpenSSH private key |
| `install/private-key.asc`, later `install/debian/private-key.asc` | GPG private key |
| `install/ssh-add_expect`, later `install/aur/tool/ssh-add_expect` | `expect` wrapper for `ssh-add`; reads the passphrase from `$SIGNING_PASSWORD` |

## Owner checklist

### AUR SSH key

1. List the keys authorized for `aur.archlinux.org` in the AUR account.
2. Remove every authorized key whose fingerprint matches the one recorded
   below.
3. Generate a new key on the machine that runs the release, add its public
   half to AUR, and add it to the repository secret `AUR_SSH_PRIVATE_KEY`.
4. Record the fingerprint and the date in the table below.

### Debian/GPG key

1. Publish a revocation certificate for the fingerprint recorded below and
   upload it to the keyservers the package users rely on.
2. Create a new signing key, update the Debian packaging and the release
   secret that holds it, and sign a fresh package.
3. Record the fingerprint and the date below.

### After both

- Confirm that secret scanning and push protection are enabled (SEC-15); the
  `check-private-keys.sh` gate refuses a new key in the tree.
- Treat the rewrite as containment, not as revocation: it keeps the keys out
  of this repository, it does not make them unusable.

## Confirmation

| Credential | Fingerprint | Revoked / replaced on | By |
| --- | --- | --- | --- |
| AUR SSH (old) | `SHA256:29ypIMy4R2GbNLQ+aoqxth0VM5NeB2TDuZRQp05HX+E` (`mzz@mzzketi`) | _pending_ | _pending_ |
| Debian GPG (old) | `29D9B7D46F20545A35859953F4FEBCD04F64D328` | _pending_ | _pending_ |
