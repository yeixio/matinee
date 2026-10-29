---
title: Updating and backups
description: Keep Matinee up to date, see which version you run, and back up what matters.
sidebar:
  order: 5
---

## Which version am I running?

The bottom of **Settings** shows the server's version, such as `Matinee server v0.1.0 · API v1`. Each release has notes under **Release notes** in this guide and on [GitHub](https://github.com/yeixio/matinee/releases).

## Updating

| How you installed | How to update |
| --- | --- |
| Docker | `docker pull ghcr.io/yeixio/matinee:latest`, then recreate the container |
| apt | `sudo apt-get update && sudo apt-get upgrade` |
| dnf | `sudo dnf upgrade matinee` |
| Homebrew | `brew upgrade matinee && brew services restart matinee` |
| Helm | `helm upgrade matinee oci://ghcr.io/yeixio/charts/matinee --version <new version> -f my-values.yaml` |

Updates move your database forward by themselves on the first start. Going back to an older version after that isn't supported, so take a backup first if you might want to.

Docker images are tagged by version (`0.1.0`, `0.1`) as well as `latest`, so you can pin a version and move up on your own schedule.

<!-- since: v0.1.4 -->
### Updating while people are watching

You don't need to wait for everyone to finish. When Matinee stops, it first tells every open player that it's restarting, lets requests in flight finish, and only then exits. Players say **The server is restarting**, wait for it, and [pick up where they were](/docs/watching/playing/#when-the-server-restarts-or-updates), so an update is a pause of however long the new version takes to start.

With Helm, the chart replaces the pod by stopping the old one before starting the new one (`strategy: Recreate`), because the home network port, remote access and the volumes it uses all belong to one pod at a time. With the bundled database and Matinee pinned to a node (`nodeSelector` or `affinity`), the chart's setup step pulls the new image onto that node before the old pod stops, which keeps the pause short. If your install uses none of those (`discovery.homePort: 0`, `remoteAccess.enabled: false`, and volumes two pods can mount at once), set `strategy.type: RollingUpdate` so the new pod starts first and the pause is only a few seconds. `shutdown.drainDelaySeconds` and `shutdown.timeoutSeconds` tune how long the old pod keeps serving.

Outside Kubernetes, `MATINEE_DRAIN_DELAY` and `MATINEE_SHUTDOWN_TIMEOUT` (or `--drain-delay` and `--shutdown-timeout`) do the same.

<!-- /since -->

## What to back up

Everything Matinee knows lives in its data folder (`MATINEE_DATA_DIR`):

| Install | Data folder |
| --- | --- |
| Docker | whatever you mounted at `/data` |
| apt, dnf | `/var/lib/matinee` |
| Homebrew | `$(brew --prefix)/var/matinee` |
| Helm | the bundled PostgreSQL database, plus the downloads volume |

What matters is the database: accounts, watch history, watch lists, collections and settings. With SQLite (the default) that's `matinee.db` in the data folder; stop Matinee before copying it, or copy it with `sqlite3 matinee.db ".backup backup.db"`. With PostgreSQL, use `pg_dump`.

The image cache and transcode scratch space in the data folder can be skipped; Matinee rebuilds them. Your media files are never changed by Matinee, so back those up however you already do.
