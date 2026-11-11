---
title: Backups
description: Matinee backs up its database every day and before each update, and lets an admin download a backup or restore one from Settings.
since: v0.2.0
sidebar:
  order: 12
---

Your accounts, watch history, ratings, watch lists, collections and settings all live in Matinee's database. With SQLite (the default), Matinee backs it up by itself, so an update or a mistake never costs you years of history.

## When Matinee backs up

- **Before an update changes the database.** When a new version needs to change the database, Matinee copies it first and says so in the log. If the copy fails (say the disk is full), the update stops and the database stays as it was, so the old version still starts.
- **On a schedule.** Every day by default. You can pick every 6 or 12 hours, every week, or only before updates.
- **When you ask.** **Back up now** makes one straight away, before a big change for example.

Backups go in the `backups` folder in the data folder, such as `/data/backups` in Docker. Matinee keeps the newest 7 of each kind and deletes older ones; you can keep more or fewer. A backup is a copy of the whole database while Matinee keeps running.

## Settings → Server health → Backups

Admins see every backup with when it was made, why, and how big it is. From there you can:

- **Download** a backup, to keep it somewhere else.
- **Restore** a backup. Matinee puts it in place of the database the next time it starts, so restart Matinee to finish<!-- until: v0.2.4 --> (in Docker, restart the container)<!-- /until --><!-- since: v0.2.4 --> with **Restart server** at the top of [Server health](/docs/server/server-health/#restart-the-server)<!-- /since -->. Anything that changed after the backup was made is lost, so the database as it was just before the restore is kept as a backup too. **Cancel restore** keeps things as they are, as long as Matinee hasn't restarted yet.
- **Delete** one you don't need.

If a backup fails, it says why there, and admins get an [alert](/docs/server/alerts/) if alerts are set up. You can turn that alert off under **Alerts** with **A backup fails**.

## Restoring by hand

If Matinee won't start at all, you can restore a backup yourself: stop Matinee, move `matinee.db` (and `matinee.db-wal` and `matinee.db-shm` if they're there) out of the data folder, copy the backup you want to `matinee.db`, and start Matinee again.

## PostgreSQL

Matinee doesn't copy a PostgreSQL database itself. Back it up with `pg_dump`, for example every night from cron:

```sh
pg_dump --format=custom --file=matinee-$(date +%F).dump "$MATINEE_DATABASE_URL"
```

and restore one into an empty database with `pg_restore --dbname="$MATINEE_DATABASE_URL" matinee-2026-11-04.dump`. On Kubernetes, the database's own backup tools (or a CronJob running `pg_dump`) do the same. Take one before an update too.
