# Rotation of the credentials exposed in git history (SEC-16)

The audit found private keys in the history of this repository. They are not
in the current tree, but the history is public: a clone contains them, and a
key is only retired when its owner revokes it, not when a commit deletes it.

## What is in the history

| File | Kind | Removed in |
| --- | --- | --- |
| `install/deploy_key` | OpenSSH private key; the path moved before deletion, the key survives as `install/aur/deploy_key` | `29022e2d`, then `6c537a32` (2021) |
| `install/aur/deploy_key` | OpenSSH private key | `6c537a32` (2021) |
| `install/debian/private-key.asc` | GPG private key | `6c537a32` (2021) |
| `install/ssh-add_expect` | `expect` script carrying a passphrase | `6c537a32` (2021) |

The commits say "rotate keys", but a commit message does not revoke anything
at the relying party. The checklist below is an owner action: only the AUR
account holder and the Debian/GPG key holder can complete it.

## Owner checklist

### AUR SSH key

1. List the keys authorized for `aur.archlinux.org` in the AUR account.
2. Print the fingerprint of the historical key **without printing the key**:

   ```sh
   # The private key is encrypted; the tracked public half gives the
   # fingerprint without needing its passphrase.
   git show 6c537a32^:install/aur/deploy_key.pub | ssh-keygen -lf -
   ```

3. Remove every authorized key whose fingerprint matches one of those.
4. Generate a new key on the machine that runs the release, add its public
   half to AUR, and replace the repository secret `AUR_SSH_PRIVATE_KEY`.
5. Record the fingerprint and the date in the table below.

### Debian/GPG key

1. Print the fingerprint of the historical key without importing it into the
   default keyring:

   ```sh
   git show 6c537a32^:install/debian/private-key.asc \
     | gpg --with-colons --import-options show-only --import | grep '^fpr:'
   ```

2. Publish a revocation certificate for that fingerprint and upload it to the
   keyservers the package users rely on.
3. Create a new signing key, update the Debian packaging and the release
   secret that holds it, and sign a fresh package.
4. Record the fingerprint and the date below.

### After both

- Confirm that secret scanning and push protection are enabled (SEC-15); the
  `check-private-keys.sh` gate refuses a new key in the tree.
- Do not rewrite history as a substitute for revocation. Rewriting hides the
  key from a fresh clone but every existing fork still has it.

## Confirmation

| Credential | Fingerprint | Revoked / replaced on | By |
| --- | --- | --- | --- |
| AUR SSH (old) | `SHA256:29ypIMy4R2GbNLQ+aoqxth0VM5NeB2TDuZRQp05HX+E` (`mzz@mzzketi`) | _pending_ | _pending_ |
| Debian GPG (old) | `29D9B7D46F20545A35859953F4FEBCD04F64D328` | _pending_ | _pending_ |
