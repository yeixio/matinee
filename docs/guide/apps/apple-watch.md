---
title: Apple Watch
description: Control whatever Matinee is playing on your phone, TV or computer from your Apple Watch, with a complication for your watch face.
since: v0.1.6
sidebar:
  order: 7
---

With the iPhone app, your Apple Watch becomes a remote for Matinee. It shows what's playing on your iPhone, your TV or a web browser, and lets you pause it, skip, turn it up or down and set the sleep timer, without reaching for anything else.

The watch app comes with the iPhone app from the App Store, version 1.0.15 or later; buying the app covers the watch too. It needs watchOS 10 or later. There's nothing to set up on the server.

## Getting started

1. Sign in to your server in the Matinee app on your iPhone. The watch uses the same account, so sign in as yourself: the shared Home account can't control players.
2. Matinee installs on your watch by itself if automatic app installs are on. If it doesn't, open the **Watch** app on your iPhone, find **Matinee** under **Available apps**, and tap **Install**.
3. Open **Matinee** on the watch, with your iPhone nearby. The iPhone signs the watch in by itself; you don't type anything on the watch.

The watch then shows up under **Settings → Your devices** with its own name (see [Your devices](/docs/apps/signing-in/#your-devices)), so you can sign it out from there like any other device. The iPhone app doesn't need to be open, then or later.

## What's on the watch

The watch follows whatever is playing. If several things are playing, tap the player's name at the top to pick one from **Players**: every Matinee app and browser that's open on your account, with what each one is playing. The watch keeps following the one you picked while it's open.

For the player it follows, the watch shows:

- What's playing, with its poster behind it and a bar for how far along it is.
- **Play** and **Pause**, and **Previous** and **Next**: the next episode for shows, the next song for music, the next chapter for books.
- **Volume** up and down, on players that have their own volume, such as a phone or a browser. Turning the Digital Crown changes the volume too. TVs use their own remote for the volume, so the watch leaves it out.
- **The sleep timer** (the moon): stop after 15 minutes to an hour and a half, or when what's playing ends. The moon lights up while a timer is set. See [Sleep timer](/docs/watching/playing/#sleep-timer).

Whoever is watching sees "Controlled from" and your watch's name for a moment, so nobody wonders why the film paused by itself.

To sign the watch out, open **Players** and tap **Sign out** at the bottom.

## The complication

The **Now playing** complication shows what's playing on your watch face, with a play or pause sign. Touch and hold the watch face, tap **Edit**, swipe to the complications, pick a spot and choose **Matinee**. It also works in the Smart Stack. Tapping it opens Matinee.

## Something isn't working

- **"Install Matinee on your phone and keep it nearby"**: the watch can't reach the Matinee app on its iPhone. Install it, and check that the watch is connected to the iPhone (Control Center on the watch shows it).
- **"Open Matinee on your phone and sign in"**: the iPhone app isn't signed in, or it uses the shared Home account. Sign in as yourself on the iPhone, then tap **Try again** on the watch.
- **"Can't reach your server"**: the watch reaches your server through your iPhone or Wi-Fi. At home that just works; away from home, the server has to be reachable from outside (see [Watching away from home](/docs/server/remote-access/)).
- **A player isn't in the list**: only players that are open show up. Open Matinee on that device, or the player page in a browser.
- **"You signed out on this watch"**: tap **Sign in with phone** to sign in again.
