---
title: Radarr and Sonarr
description: Connect Radarr and Sonarr so people can ask for movies and shows, see what's coming, and report problems that get fixed.
sidebar:
  order: 6
---

If you use [Radarr](https://radarr.video/) for movies or [Sonarr](https://sonarr.tv/) for TV, Matinee can work with them. People ask for titles from Matinee, see what's on its way, and report bad files, and Radarr and Sonarr do the downloading.

## Connecting

1. Open **Settings → Add-ons → Radarr and Sonarr** and click **Connect** next to Radarr (or Sonarr).
2. Enter its address, such as `http://radarr:7878`, and its API key. You'll find the key in Radarr or Sonarr under **Settings → General**.
3. Click **Test**, then pick the quality profile and root folder that new titles get, and **Save**.

![Connecting Radarr: address, API key, quality profile and folder](../images/radarr-connect.webp)

The address and key stay on the server; nobody's browser or app ever sees them.

## Disconnecting

To stop using Radarr or Sonarr (for example after you turn it off, or move to Box Office), click **Disconnect** next to it under **Settings → Add-ons** and confirm. On a phone it's the unplug button. It works even when the app isn't answering any more. When Box Office is connected, the Radarr and Sonarr entries say **Movies go through Box Office** and **Shows go through Box Office**. That's Box Office's own connection, not the Radarr or Sonarr apps, so they have no Disconnect of their own: disconnect Box Office instead. Titles already in Radarr or Sonarr stay there, and asking for titles and **Coming soon** turn off until something is connected again.

<!-- since: v0.2.3 -->
## Box Office

[Box Office](https://github.com/yeixio/boxoffice) finds and downloads movies and shows, and answers Matinee the way Radarr and Sonarr do. It has its own entry, first in the list, so one connection does both jobs:

1. Open **Settings → Add-ons**. Matinee looks for Box Office as soon as the page opens, and when it finds one, **Box Office** says *Found on your network* with its address. Click **Connect**: the address is already filled in. Change it if you'd rather use another, or click another one Matinee found.
2. Paste Box Office's API key. In Box Office, sign in as an admin, open **Settings → Connect Matinee**, and use the copy button next to **API key**.
3. Click **Test**, then **Save**.

Saving points Radarr at `<address>/radarr` and Sonarr at `<address>/sonarr` with that key, keeping the quality profile and root folder they had when Box Office has them, else Box Office's first. Change those under Radarr and Sonarr below. If Box Office has no TV (or movie) root folder yet, only the other kind connects until you add one and save again. Disconnecting Box Office disconnects both.

### How Matinee finds Box Office

Matinee asks each of these places whether it's Box Office, and nothing goes through Yeix or any other service. It tries, in this order:

- wherever Radarr or Sonarr already go;
- `boxoffice:7272`, its name in Kubernetes or Docker Compose, and port 7272 on the same machine;
- `boxoffice.` beside Matinee's own domain, such as `https://boxoffice.example.com` for `https://matinee.example.com`;
- Box Office announcing itself on the home network (it does this on its own, as `_boxoffice._tcp`, unless it's started with `BOXOFFICE_ANNOUNCE=false`);
- port 7272 on the other computers on Matinee's own home network (the 256 addresses around its own).

Only an address that answers as Box Office is listed. If Box Office listens on another port or Matinee runs somewhere it can't see the home network, such as inside Kubernetes without Box Office beside it, type the address.
<!-- /since -->

## Who can add titles

Under **Who can add movies and shows**, pick one:

| Choice | What happens |
| --- | --- |
| **Ask, then an admin approves** (the default) | Anyone with an account can ask for a title. It waits for an admin. |
| **Add right away** | What people ask for goes straight to Radarr or Sonarr. |
| **Only admins add titles** | Everyone else can still see what's coming soon and report problems. |

## Requests

**Requests** (the inbox icon in the top bar) has up to three tabs:

- **Ask for something:** search for any movie or show and ask for it (or **Add** it, when requests are added right away).
- **Requests:** admins see everyone's requests here and **Approve** or **Decline** them. Everyone else sees **Your requests** and where each one is up to.
- **Problems:** reports from **Report a problem** on a title's page. An admin can **Close** one once it's sorted, or click **Replace file**, which deletes the file in Radarr or Sonarr and has it download another copy. Everyone else sees **Your reports**.

### Problems Box Office fixes by itself

<!-- since: v0.2.4 -->
When [Box Office](https://github.com/yeixio/boxoffice) is connected, a problem with the copy itself fixes itself. On a movie's or episode's page, **Report a problem** and pick what's wrong:

| Problem | What Box Office does |
| --- | --- |
| **Picture** or **Sound** | Blocks the release the copy came from and gets another. |
| **Wrong language** | Asks which language it should be in (your app's language is picked to start with). Box Office skips releases whose names say they're only in other languages, prefers ones that name yours, and turns down a download whose sound turns out to be in another language again, trying the next one. |
| **Wrong title** | The file is another movie or episode: Box Office blocks it and gets the right one. If the file isn't the copy Box Office has, Matinee has probably matched the title wrong, so nothing is replaced and the report waits for an admin, who can use **Fix match**. |

The old copy moves to Box Office's recycle folder once the new one is in, so a mistaken report is easy to undo. When the new copy arrives, the report closes by itself and whoever reported it is told in the bell (and on their phone, if they [get notifications](/docs/apps/notifications/)). Until then, **Requests → Problems** says Box Office is getting another copy, and Box Office shows what was reported, and by whom, on the movie's or show's page.

Reports go to Box Office straight away, whoever makes them and whatever **Who can add movies and shows** says, since a bad copy isn't a new title. To check each one first, turn on **Other editions and better copies always need an admin's approval** under **Settings → Add-ons → Box Office**: reports then wait under **Requests**, and approving one sends it. Admins' own reports always go straight through.

**Subtitles** and **Something else** still go to an admin, as do reports for titles Box Office doesn't have, and every report when Box Office isn't connected. For subtitles, try [finding subtitles](/docs/watching/finding-subtitles/) in the player first.
<!-- /since -->

## Discover from Box Office

<!-- since: v0.2.3 -->
When Box Office is connected, everyone gets the [Discover](/docs/watching/discover/) tab on the web and in the phone and TV apps, with nothing else to set up: trending, popular and upcoming titles, recommendations for what they watched and for what's in your libraries, and a search for any movie or show. Asking for a title there is an ordinary request through Box Office, so **Who can add movies and shows** applies. Kids profiles don't see Discover. <!-- until: v0.2.4 -->If [Overseerr](/docs/server/overseerr/) is also connected, Discover comes from Overseerr instead. <!-- /until -->Disconnecting Box Office takes the tab away.
<!-- /since -->

<!-- since: v0.2.4 -->
Box Office comes first: if [Overseerr](/docs/server/overseerr/) is connected too, Discover still comes from Box Office and Overseerr isn't used. Its entry under **Settings → Add-ons** says so, and you can disconnect it. Servers without Box Office keep using Overseerr as before.

### Requests on Box Office's Requests page

When Box Office takes requests, asking for a movie or show in Matinee, from Discover or from **Requests → Ask for something**, on the web or in the phone and TV apps, files it on Box Office's **Requests** page under the person's Matinee account. That's the same account they'd use to sign in to Box Office's request site, so everything they've asked for is in one place, whichever way they asked.

- **Box Office decides.** Its rules apply: who's approved automatically, and weekly limits, both set on its **Requests** page. If Box Office says no, for example because someone reached their weekly limit, they see why straight away.
- **Admins' requests are added straight away,** and so is everyone's when **Who can add movies and shows** says requests are added right away.
- **Approve or decline in either place.** An admin can use Box Office's **Requests** page or Matinee's **Requests** tab, which passes the decision on. Matinee catches up with what's decided on Box Office within half a minute, and tells the person who asked in the bell.
- **Deleting a request that's still waiting** takes it off Box Office's page too.

Nothing needs setting up, and Box Office's request site doesn't need to be turned on. Without a Box Office that takes requests, requests work as described above.
<!-- /since -->

## Notifications from Box Office

When Radarr or Sonarr is [Box Office](https://github.com/yeixio/boxoffice), Matinee tells people in the web player and the phone and TV apps when something they asked for is found and downloading, and when it's ready to watch. The bell in the top bar lists them. There's nothing else to set up: Matinee checks Box Office every half a minute with the address and key you already gave it.

Admins also hear about downloads that fail or look unsafe. Under **Settings → Add-ons → Radarr and Sonarr**, pick what admins hear about: **Problems** (the default), **Everything**, or **Only their own**.

<!-- since: v0.2.3 -->
On phones and tablets, people can also get these as the device's own notifications with the app closed: see [Notifications on your phone](/docs/apps/notifications/).
<!-- /since -->

<!-- since: v0.2.3 -->
The same notification again, such as Box Office saying a disk is still nearly full, doesn't stack up in the bell. It goes back to the top of the list as unread, with the newest wording and how many times it's come, like **12×, first 3 days ago**. It only pops up again if it gets more serious, from a warning to critical. Clear it, and the next one starts a new count. On phones, a repeat doesn't buzz again either.
<!-- /since -->

## Coming soon

**Coming soon** (the calendar icon) lists new episodes and movie releases due in the next two months that aren't in your library yet. The same titles appear in a row on [Home](/docs/watching/home/), and in the [phone and TV apps](/docs/apps/phone-and-tv/).

## On its way and Other copies

<!-- since: v0.2.3 -->
When Radarr or Sonarr is [Box Office](https://github.com/yeixio/boxoffice), Home has an **On its way** row on the web and in the phone and TV apps: what Box Office is looking for and downloading, greyed out with a progress bar, where each one is up to and about how long it has left. Everyone sees it, so **Notify me** under a title someone else asked for (on a TV, select it) tells you when it's ready too.

A movie's or episode's page has **Other copies** for people who can ask for titles:

- **Other editions** of a movie, such as a Director's Cut, added alongside the copy there or in its place.
- **Ask for a better copy**, for when a better release is out.
- **This copy is bad**, saying what's wrong. On the web you write it; the phone and TV apps offer **Wrong language**, **Out of sync**, **Poor picture** or **Something else**, so nobody types with a remote. On a TV, Other copies is on the stars' line, under the title's buttons.

These are requests like any other, so **Who can add movies and shows** applies, and an admin can make them always wait for approval. Without Box Office, neither shows.
<!-- /since -->

## Changing quality

On a movie or show's page, admins can change the quality profile Radarr or Sonarr uses for it, for example to upgrade a favorite to 4K.

:::note
Devices at home that haven't signed in can see **Coming soon**, but can't ask for titles or report problems. See [Home network](/docs/server/home-network/).
:::
