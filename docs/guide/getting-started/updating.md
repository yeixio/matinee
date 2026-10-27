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
