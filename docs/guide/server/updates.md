---
title: Updates
description: Matinee looks for new versions by itself, from releases or from main as a beta, and installs them automatically or when you say, wherever it can.
since: v0.2.3
sidebar:
  order: 12
---

Matinee looks for a newer version every hour and shows it in **Settings → Server health → Updates**, with a link to what's new. Where it can install updates itself, it does so when you press **Update**, or by itself if you turn on **Install updates automatically**. Installing restarts the server, so an automatic update waits until nobody is watching or listening; Settings says when one is waiting.

## Releases or main

**Releases** are the tagged versions, such as v0.3.0, each with its release notes. They're the default and what most servers should follow.

**Main** is a beta, and a risky one: every change as soon as it passes its checks, before anyone has used it. You get new things first, and now and then something that's broken. Builds of main are made only as container images, so only servers that run the image (Docker or Kubernetes) can follow it. Settings shows a build of main as `main-<commit>`.

Matinee never offers an older version. A server on a build of main that switches to releases stays where it is until a release comes out with everything it has.

Your [backups](/docs/server/backups/) keep you covered: Matinee backs up its database before a new version changes it.

## Who installs the update

It depends on how you installed Matinee, and Settings says which applies to your server.

| Installed with | How it updates |
| --- | --- |
| Kubernetes, through Argo CD | Matinee moves its Argo CD Application to the new version and Argo CD rolls it out. Needs a little setup; see below. |
| Kubernetes, with Helm | `helm upgrade` with the new chart version. Matinee shows what's new. |
| Docker, Compose, a NAS app store | A container can't replace itself: pull the new image and recreate the container (`docker compose pull && docker compose up -d`). The `latest` tag follows releases and `main` follows main. Watchtower, or your NAS's app store, can do it automatically. |
| apt or dnf | Matinee asks the small helper that comes with its package to upgrade it, and the helper has apt or dnf upgrade the `matinee` package and nothing else. See below. By hand: `sudo apt update && sudo apt upgrade matinee` or `sudo dnf upgrade matinee`. |
| Homebrew | Matinee runs `brew upgrade matinee` as the user it runs as, then restarts into the new version. |
| Matinee Server for Mac, the Windows installer | The app updates itself. |
| The download for Linux or macOS | Matinee downloads the new version, checks it against the release's checksums, puts it in place of its own file and restarts. It needs to be allowed to write to its own folder. |

When Matinee restarts into a new version, the apps say so and pick up where they were once it's back.

If your server can't install updates itself, Matinee can still tell you about them: leave the channel on **Releases** and check Settings now and then, or turn on the restart [alert](/docs/server/alerts/) to hear when it comes back on a new version.

## apt and dnf

The server runs as its own `matinee` user, which can't install packages. The package adds a helper for that: `matinee-update.path`, a systemd unit that watches `/var/lib/matinee/update/request`. When Matinee writes there, the helper runs as root, upgrades only the `matinee` package from the repository you installed it from, and the package restarts the server. It writes what happened to `/var/log/matinee-update.log`, and Settings shows the error if it didn't work.

To keep updates in your own hands, turn the helper off. It stays off through later upgrades, and Settings shows the command to run instead:

```sh
sudo systemctl disable --now matinee-update.path
```

## Kubernetes with Argo CD

Matinee updates itself by changing the Argo CD Application it's installed from: it sets the chart's revision (`spec.source.targetRevision`) and the `image.tag` parameter. Two things make that work.

Tell the chart which Application it is, so Matinee may read and change it (only that one):

```yaml
helm:
  valuesObject:
    updates:
      argoCD:
        application: matinee   # its name
        namespace: argocd      # where Argo CD keeps Applications
```

If another Argo CD Application manages Matinee's Application (an app of apps), tell it to leave those two fields alone, or it puts them back:

```yaml
spec:
  ignoreDifferences:
    - group: argoproj.io
      kind: Application
      name: matinee
      jsonPointers:
        - /spec/source/targetRevision
        - /spec/source/helm/parameters
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
```

Then Settings works like this:

- **Main, installed automatically**: the Application follows the `edge` branch with the image built from each commit, so every build of main rolls out within minutes of passing its checks.
- **Main, installed by hand**: the Application stays on the commit you run until you press **Update**.
- **Releases**: the Application stays on the release you run (such as `v0.3.0`, with image `0.3.0`), and moves to the next one automatically or when you press **Update**.

Settings shows what the Application installs now. Until you change anything, an Application that follows `edge` counts as main, installed automatically, since that's what it does.
