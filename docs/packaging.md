# Packaging

A stable release (a `v*` tag with no suffix) ships Matinee five more ways besides the image and the Helm chart:

| Channel | Built by | Published to |
| --- | --- | --- |
| `.deb` for amd64, arm64, armhf | `scripts/package-linux.sh` (nfpm) | the GitHub release, and the apt repository |
| `.rpm` for x86_64, aarch64 | `scripts/package-linux.sh` (nfpm) | the GitHub release, and the dnf/yum repository |
| apt and dnf repositories | `scripts/package-repos.sh` | the `packages` branch, read through raw.githubusercontent.com |
| Homebrew formula (macOS, Linux) | `scripts/write-brew-formula.sh` from `packaging/homebrew/matinee.rb.tmpl` | `Formula/matinee.rb` on `main`, through a pull request from the `formula` branch, so this repository is its own tap |
| Windows installers for x64 and arm64 | `scripts/package-windows.sh` (NSIS) from `packaging/windows/matinee.nsi` | the GitHub release, as `Matinee-Setup-x64.exe` and `Matinee-Setup-arm64.exe` |

Pre-releases attach the `.deb` and `.rpm` files to their GitHub release but don't touch the repositories or the formula.

## What the Linux packages install

- `/usr/bin/matinee`
- `/usr/lib/systemd/system/matinee.service`, enabled and started on install, restarted on upgrade, stopped on removal
- `/etc/matinee/matinee.env`, the settings file, kept across upgrades
- a `matinee` system user, in the `render` and `video` groups for hardware transcoding, with its data in `/var/lib/matinee`, which removal leaves in place
- a dependency on FFmpeg: `ffmpeg` on Debian and Ubuntu, `/usr/bin/ffmpeg` and `/usr/bin/ffprobe` on Fedora and RHEL, which Fedora's `ffmpeg-free` and RPM Fusion's `ffmpeg` both provide

## Signing

`PACKAGE_SIGNING_KEY` (an armored secret key) and `PACKAGE_SIGNING_PASSPHRASE` sign the apt index (`InRelease`, `Release.gpg`), every rpm, and the rpm index (`repomd.xml.asc`). The public half is published as `matinee.asc` on the `packages` branch. A stable release fails without the key, so the repositories are never published unsigned.

The current key is "Matinee packages <hello@yeix.io>", RSA 4096, fingerprint `4EE3 3F8C EE07 6BB0 C9A7  C559 21F5 BE9F 726A 2895`, made 2026-10-26 and valid until 2031-10-25. Its passphrase and an armored backup are in the maintainer's macOS login keychain ("Matinee package signing passphrase", "Matinee package signing key").

The key is RSA rather than Ed25519, because the rpm on RHEL 8 and its rebuilds can't check Ed25519 signatures. To make one:

```sh
export GNUPGHOME="$(mktemp -d)"
gpg --batch --passphrase 'a long passphrase' --quick-generate-key "Matinee packages <hello@yeix.io>" rsa4096 sign 5y
gpg --list-keys --with-fingerprint                       # note the fingerprint
gpg --batch --pinentry-mode loopback --passphrase 'a long passphrase' --armor --export-secret-keys > matinee-signing.asc
```

Save `matinee-signing.asc` as the `PACKAGE_SIGNING_KEY` repository secret and the passphrase as `PACKAGE_SIGNING_PASSPHRASE` (Settings → Secrets and variables → Actions), keep an offline copy, then delete the file and `$GNUPGHOME`.

## Windows

`scripts/package-windows.sh` builds one installer per architecture on Linux with NSIS. Each holds:

- `matinee.exe`, the server
- `matinee-tray.exe` (`cmd/matinee-tray`), the app that runs it from the notification area: start and stop, open in the browser, start at sign-in, logs, and updates from the GitHub release (checked against `SHA256SUMS.txt`)
- FFmpeg's GPL build for Windows from [BtbN/FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds), the 8.1 release branch, with its licence and where its source is

The installer goes in Program Files for everyone on the PC, adds a Start menu shortcut, and lets `matinee.exe` through Windows Firewall on private and domain networks. Each person who runs it gets their own library in `%LOCALAPPDATA%\Matinee`. The tray app runs the server as that person (never as the administrator who installed it), so it sees their mapped drives and the share passwords Windows remembers for them. It stops the server by closing its stdin (`MATINEE_STOP_ON_STDIN_CLOSE`), since Windows has no SIGTERM.

`/S` installs silently; `/S /update` also starts Matinee afterwards, which is how the tray app updates. CI installs both installers on fresh Windows x64 and Arm runners, runs Matinee, and uninstalls it (`scripts/test-windows-install.ps1`), saving screenshots of the tray menu as the `windows-shots-*` artifacts.

### Code signing

Unsigned, the installer gets a Microsoft Defender SmartScreen warning. The release signs the programs, the installer and its uninstaller with [Azure Artifact Signing](https://azure.microsoft.com/products/artifact-signing) (formerly Trusted Signing) once these are set in Settings → Secrets and variables → Actions:

| Name | Kind | What |
| --- | --- | --- |
| `AZURE_SIGNING_TENANT_ID` | secret | the Microsoft Entra tenant |
| `AZURE_SIGNING_CLIENT_ID` | secret | an app registration with the **Artifact Signing Certificate Profile Signer** role on the signing account |
| `AZURE_SIGNING_CLIENT_SECRET` | secret | that app registration's client secret |
| `AZURE_SIGNING_ENDPOINT` | variable | the account's endpoint without `https://`, such as `eus.codesigning.azure.net` |
| `AZURE_SIGNING_ACCOUNT` | variable | the signing account's name; signing is on when it's set |
| `AZURE_SIGNING_PROFILE` | variable | the public-trust certificate profile's name |

`scripts/sign-windows.sh` signs with [jsign](https://github.com/ebourg/jsign), so it runs on Linux.

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

## NAS app stores

`packaging/` also holds Matinee's entries for NAS app stores. Each store keeps its own copy, so a new release means a pull request to that store with the new image tag and digest (`docker buildx imagetools inspect ghcr.io/yeixio/matinee:<version>` prints the digest).

| Folder | Store | Where it goes |
| --- | --- | --- |
| `unraid/` | Unraid Community Applications | Served from this repository; the template points at `:latest`, so it needs no update per release |
| `umbrel/matinee/` | Umbrel App Store | `getumbrel/umbrel-apps`, as the `matinee/` folder |
| `zimaos/Matinee/` | ZimaOS / CasaOS App Store | `IceWhaleTech/CasaOS-AppStore`, under `Apps/` |
| `truenas/matinee/` | TrueNAS Apps (community train) | `truenas/apps`, under `ix-dev/community/` |

Before sending a store an update, test it the way that store asks: Umbrel through umbrelOS (a device or its container image), TrueNAS with the repository's own test runner, ZimaOS with `scripts/build_dist.sh`.
