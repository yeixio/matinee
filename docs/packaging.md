# Packaging

A stable release (a `v*` tag with no suffix) ships Matinee four more ways besides the image and the Helm chart:

| Channel | Built by | Published to |
| --- | --- | --- |
| `.deb` for amd64, arm64, armhf | `scripts/package-linux.sh` (nfpm) | the GitHub release, and the apt repository |
| `.rpm` for x86_64, aarch64 | `scripts/package-linux.sh` (nfpm) | the GitHub release, and the dnf/yum repository |
| apt and dnf repositories | `scripts/package-repos.sh` | the `packages` branch, read through raw.githubusercontent.com |
| Homebrew formula (macOS, Linux) | `scripts/write-brew-formula.sh` from `packaging/homebrew/matinee.rb.tmpl` | `Formula/matinee.rb` on `main`, through a pull request from the `formula` branch, so this repository is its own tap |

Pre-releases attach the `.deb` and `.rpm` files to their GitHub release but don't touch the repositories or the formula.

## What the Linux packages install

- `/usr/bin/matinee`
- `/usr/lib/systemd/system/matinee.service`, enabled and started on install, restarted on upgrade, stopped on removal
- `/etc/matinee/matinee.env`, the settings file, kept across upgrades
- a `matinee` system user, in the `render` and `video` groups for hardware transcoding, with its data in `/var/lib/matinee`, which removal leaves in place
- a dependency on FFmpeg: `ffmpeg` on Debian and Ubuntu, `/usr/bin/ffmpeg` and `/usr/bin/ffprobe` on Fedora and RHEL, which Fedora's `ffmpeg-free` and RPM Fusion's `ffmpeg` both provide

## Signing

`PACKAGE_SIGNING_KEY` (an armored secret key) and `PACKAGE_SIGNING_PASSPHRASE` sign the apt index (`InRelease`, `Release.gpg`), every rpm, and the rpm index (`repomd.xml.asc`). The public half is published as `matinee.asc` on the `packages` branch. A stable release fails without the key, so the repositories are never published unsigned.

The key is RSA rather than Ed25519, because the rpm on RHEL 8 and its rebuilds can't check Ed25519 signatures. To make one:

```sh
export GNUPGHOME="$(mktemp -d)"
gpg --batch --passphrase 'a long passphrase' --quick-generate-key "Matinee packages <hello@yeix.io>" rsa4096 sign 5y
gpg --list-keys --with-fingerprint                       # note the fingerprint
gpg --batch --pinentry-mode loopback --passphrase 'a long passphrase' --armor --export-secret-keys > matinee-signing.asc
```

Save `matinee-signing.asc` as the `PACKAGE_SIGNING_KEY` repository secret and the passphrase as `PACKAGE_SIGNING_PASSPHRASE` (Settings → Secrets and variables → Actions), keep an offline copy, then delete the file and `$GNUPGHOME`.

## Testing

`scripts/test-packages.sh` serves the repositories locally and, in fresh Debian, Ubuntu and Fedora containers, adds them the way the README says (checking signatures, never `trusted=yes` or `gpgcheck=0`), installs Matinee with its FFmpeg dependency, starts it and checks `/healthz`. CI runs it on every pull request with a throwaway key, and a stable release runs it on the repositories it's about to publish.

Locally, with nfpm, dpkg-dev, apt-utils, createrepo-c, rpm and Docker:

```sh
make web
VERSION=0.0.0-dev TARGETS=linux/amd64 scripts/release-binaries.sh
VERSION=0.0.0-dev scripts/package-linux.sh
scripts/test-packages.sh
```

## Hosting

The repositories live on the `packages` branch, replaced by one commit each release so old packages don't pile up in git history, and are served by raw.githubusercontent.com. That needs no setup beyond this repository being public. If downloads outgrow it, the same directory can be served from GitHub Pages or a CDN under a project domain: set `PACKAGES_URL` for `scripts/package-repos.sh` and update the URLs in the README.
