---
title: Wear OS watches
description: Control whatever Matinee is playing from a Wear OS watch, or download music and audiobooks to the watch and listen without your phone.
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

<!-- since: v0.1.6 -->

## Listening on the watch

Put albums and audiobooks on the watch and listen with Bluetooth headphones, with your phone left at home: on a run, at the gym, or in bed. This needs the app's version 1.0.15 or later and Matinee 0.1.6 or later on the server.

### Adding music and books

1. On the watch, tap the player's name at the top to open **Players**, then **On this watch**. (When nothing's playing, **On this watch** is right on the first screen.)
2. Tap **Add music and books**. Search for an album or a book by its name or by who made it, or pick from **Continue listening**, **New albums** and **New audiobooks**.
3. Tap one to download it. It shows up under **On this watch** with how far along it is.

Downloads carry on in the background with the app closed, and pick up where they stopped if the connection drops. They're much quicker over Wi-Fi than through your phone, so leave the watch on its charger at home for a big audiobook. Each file is checked once it's on the watch, so a broken download never plays.

Files the watch plays as they are, such as MP3, AAC, FLAC and Opus, arrive as they are. Anything else is turned into AAC by the server first, which takes a moment longer.

Downloads belong to the watch's own sign-in, like the phone's [downloads](/docs/apps/downloads/), so they're yours. To take one off the watch, tap it under **On this watch**, then **Remove from watch**. Signing the watch out removes them all.

### Playing

Tap an album or a book under **On this watch**, then **Play**. The watch switches to **This watch**, its own player, which you can also pick in **Players** at any time; pick another player to go back to being a remote.

- **Music** plays from the first song, with **Previous** and **Next**.
- **Books** start where you left off, on any device, and show the chapter you're in, with back 15 seconds and ahead 30 seconds.
- Turning the crown, or the volume buttons, sets the watch's volume.
- **The sleep timer** works the same as for other players, and stops the watch's player.

It plays to Bluetooth headphones or speakers, not the watch's own speaker. If none are connected, the watch says so and offers **Bluetooth settings** to connect a pair; the music starts once they're connected.

What plays also shows in the watch's media controls, so you can pause it from there with Matinee closed.

### Your place in a book

The watch keeps your place in a book as you listen, with no connection needed, and sends it to your server the next time it can reach it. The most recent place wins: if you listened further on your phone in the meantime, that place stays. Finishing a book on the watch counts as finishing it everywhere.

<!-- /since -->

## The tile and the complication

- **The Now playing tile** shows what's playing, with **Play** or **Pause** and **Next**. Add it by swiping to the end of your tiles and tapping **+**.
- **The Now playing complication** shows the title on your watch face, with a play or pause sign. Add it from the watch face's settings. Tapping it opens Matinee.

## Something isn't working

- **"Install Matinee on your phone and keep it nearby"**: the watch can't find the Matinee app on its phone. Install it, and check that the watch is connected to the phone in the Wear OS app.
- **"Open Matinee on your phone and sign in"**: the phone app isn't signed in, or it uses the shared Home account. Sign in as yourself on the phone, then tap **Try again** on the watch.
- **"Can't reach your server"**: the watch reaches your server through your phone or Wi-Fi. At home that just works; away from home, the server has to be reachable from outside (see [Watching away from home](/docs/server/remote-access/)).
- **A player isn't in the list**: only players that are open show up. Open Matinee on that device, or the player page in a browser.
- **"You signed out on this watch"**: tap **Sign in with phone** to sign in again.

<!-- since: v0.1.6 -->

- **A download says "Couldn't download"**: the server couldn't prepare it, or the transfer kept breaking. Tap it and then **Try again**.
- **"Connect Bluetooth headphones to listen"**: Wear OS doesn't play music through the watch's speaker. Tap the headphones button to connect a pair.

<!-- /since -->
