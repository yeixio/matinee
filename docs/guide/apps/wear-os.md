---
title: Wear OS watches
description: Control whatever Matinee is playing on your phone, TV or computer from a Wear OS watch, with a tile and a complication for quick access.
since: v0.1.5
sidebar:
  order: 6
---

With the Android app on your phone, a Wear OS watch becomes a remote for Matinee. It shows what's playing on your phone, your TV or a web browser, and lets you pause it, skip, turn it up or down and set the sleep timer, without reaching for anything else.

The watch app comes with the Android app from Google Play, version 1.0.14 or later; buying the app covers the watch too. There's nothing to set up on the server.

## Getting started

1. Sign in to your server in the Matinee app on your phone. The watch uses the same account, so sign in as yourself: the shared Home account can't control players.
2. On the watch, open the Play Store, find **Matinee** under the apps on your phone, and install it.
3. Open **Matinee** on the watch, with your phone nearby. The phone signs the watch in by itself; you don't type anything on the watch.

The watch then shows up under **Settings → Your devices** with its own name (see [Your devices](/docs/apps/signing-in/#your-devices)), so you can sign it out from there like any other device. The phone app doesn't need to be open, then or later.

## What's on the watch

The watch follows whatever is playing. If several things are playing, tap the player's name at the top to pick one from **Players**: every Matinee app and browser that's open on your account, with what each one is playing. The watch keeps following the one you picked while it's open.

For the player it follows, the watch shows:

- What's playing, with its poster behind it and a ring around the edge for how far along it is.
- **Play** and **Pause**, and **Previous** and **Next**: the next episode for shows, the next song for music, the next chapter for books.
- **Volume** up and down, on players that have their own volume, such as a phone or a browser. Turning the watch's crown changes the volume too. TVs use their own remote for the volume, so the watch leaves it out.
- **The sleep timer**: stop after 15 minutes to an hour and a half, or when what's playing ends. The moon lights up while a timer is set. See [Sleep timer](/docs/watching/playing/#sleep-timer).

Whoever is watching sees "Controlled from" and your watch's name for a moment, so nobody wonders why the film paused by itself.

## The tile and the complication

- **The Now playing tile** shows what's playing, with **Play** or **Pause** and **Next**. Add it by swiping to the end of your tiles and tapping **+**.
- **The Now playing complication** shows the title on your watch face, with a play or pause sign. Add it from the watch face's settings. Tapping it opens Matinee.

## Something isn't working

- **"Install Matinee on your phone and keep it nearby"**: the watch can't find the Matinee app on its phone. Install it, and check that the watch is connected to the phone in the Wear OS app.
- **"Open Matinee on your phone and sign in"**: the phone app isn't signed in, or it uses the shared Home account. Sign in as yourself on the phone, then tap **Try again** on the watch.
- **"Can't reach your server"**: the watch reaches your server through your phone or Wi-Fi. At home that just works; away from home, the server has to be reachable from outside (see [Watching away from home](/docs/server/remote-access/)).
- **A player isn't in the list**: only players that are open show up. Open Matinee on that device, or the player page in a browser.
- **"You signed out on this watch"**: tap **Sign in with phone** to sign in again.
