# Matinee Server for Mac

`macos/` is a small SwiftUI menu-bar app, **Matinee Server.app**, that runs the
Matinee server on a Mac. It's handed out as a Developer ID signed, notarized
disk image (not through the Mac App Store, whose sandbox would keep the server
from reading media wherever the person points it). The user guide page is
`docs/guide/getting-started/mac.md`.

## What's inside

```
Matinee Server.app/Contents/
  MacOS/Matinee Server     the menu-bar app (macos/Sources)
  MacOS/matinee            the server, universal
  MacOS/ffmpeg, ffprobe    static, universal (scripts/build-mac-ffmpeg.sh)
  Frameworks/Sparkle.framework   updates
  Resources/FFmpeg.txt     FFmpeg's version, configuration and licence
```

The app starts `matinee` with `MATINEE_DATA_DIR=~/Library/Application Support/Matinee Server`,
the bundled FFmpeg, the port and name from its settings, then anything in
`matinee.env` in that folder. The log is `~/Library/Logs/Matinee Server/matinee.log`.
Network shares the person adds are mounted with NetFS before every start.

## Building

On a Mac with Xcode, Go, Node and pnpm (and `brew install nasm pkgconf` for FFmpeg):

```sh
make web
VERSION=0.0.0-dev scripts/build-mac-app.sh   # dist/mac/Matinee Server.app and Matinee-Server.dmg
swift test --package-path macos
```

Without the signing variables the app is signed ad hoc and has no updates.
`"dist/mac/Matinee Server.app/Contents/MacOS/Matinee Server" --snapshots <dir>`
draws the menu and windows into PNGs for the guide (CI uploads them as the
`mac-snapshots` artifact).

## Releasing

`.github/workflows/mac.yml` builds the app on pull requests that touch it and,
called from Release, signs, notarizes and attaches `Matinee-Server.dmg` and
`appcast.xml` to the release. The app reads updates from
`https://github.com/yeixio/matinee/releases/latest/download/appcast.xml`, so
each stable release is the update for everyone on an older one.

A release without these secrets goes ahead without the Mac app and warns which
are missing:

| Secret | What it is |
| --- | --- |
| `DEVELOPER_ID_P12_BASE64` | A **Developer ID Application** certificate with its private key, exported as .p12, base64 |
| `DEVELOPER_ID_PASSWORD` | The .p12's password |
| `ASC_API_KEY_ID`, `ASC_API_ISSUER_ID`, `ASC_API_KEY_P8_BASE64` | An App Store Connect API key for notarizing (the same organization secrets the apps use) |
| `SPARKLE_PRIVATE_KEY` | The update signing key |

### The Developer ID certificate

Only the Apple Developer account holder can make one. In Xcode: **Settings →
Accounts →** the team **→ Manage Certificates… → + → Developer ID Application**.
Then in Keychain Access, under **My Certificates**, right-click
"Developer ID Application: …" → **Export…** as .p12 with a password, and:

```sh
base64 -i DeveloperID.p12 | gh secret set DEVELOPER_ID_P12_BASE64 -R yeixio/matinee
gh secret set DEVELOPER_ID_PASSWORD -R yeixio/matinee     # paste the password
```

(Or set both as yeixio organization secrets shared with yeixio/matinee.)

### The update key

```sh
swift macos/Resources/sparkle.swift new-key | gh secret set SPARKLE_PRIVATE_KEY -R yeixio/matinee
```

Keep a copy somewhere safe (a password manager): if it's lost, people on the
old key can't update and have to download the app again. The public half is
worked out from it at build time. A key from Sparkle's own `generate_keys -x`
works too.
