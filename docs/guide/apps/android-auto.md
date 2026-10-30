---
title: Android Auto
description: Play your music and audiobooks in the car with Android Auto, from the car's screen, its buttons or by voice, picking up where the phone left off.
since: v0.1.5
sidebar:
  order: 5
---

With the Android app on your phone, Matinee shows up in Android Auto with your music and audiobooks. You can browse and play from the car's screen, use the car's buttons, or ask for something out loud. Videos never play in the car.

This needs the Android app version 1.0.14 or later, signed in to your server. There's nothing to set up on the server.

## Getting started

1. Sign in to your server in the Matinee app on your phone. The car uses the same server and the same account, or the profile you picked. Signing in again isn't needed for the car.
2. Connect your phone to the car, by cable or wirelessly.
3. Open **Matinee** from the car's list of media apps.

The phone can stay in your pocket with the app closed: the car starts Matinee by itself, and finds your server on its own, at home or away (see [Watching away from home](/docs/server/remote-access/)). If you watch at home without signing in, as the shared Home account, sign in on the phone first: the Home account only works on your home network.

## What's in the car

Matinee has up to four tabs along the top of the car's screen:

- **Listen now**: the books you're partway through, then the albums you've played lately.
- **Music**: **Shuffle all**, every **Artist** and **Album**, and the albums added lately. Pick an album to see its songs; pick a song to play the album from there.
- **Audiobooks**: every book, with how far along you are in each.
- **Downloads**: the songs and books downloaded to your phone (see [Downloads](/docs/apps/downloads/)). These play with no connection, so they still work where the signal drops. The tab shows up once something is downloaded.

Use the car's **search** button to find an artist, album, song or book by name.

## Asking out loud

Ask the car's assistant to play something on Matinee:

- "Play *album or artist* on Matinee"
- "Play *book title* on Matinee"
- "Play Matinee" picks up whatever you last played, or shuffles your music if there's nothing to pick up.

## Listening to a book

A book picks up exactly where you left off, on your phone, the web or a TV. The car shows three buttons for books:

- **Back 15 seconds** and **Ahead 30 seconds**.
- **Speed**, which steps through the speeds from 0.75× to 3×. It's the same speed the phone app uses for your books.

**Next** and **Previous** move by chapter. Your place is saved every few seconds, so it's there when you open the book again at home.

## The phone and the car together

The car and the phone share one player. If music is playing on the phone when you get in, the car shows it and its buttons control it. With the app open on the phone, what you pick in the car plays there too, so the phone shows the same song or book.

If the server restarts, or the car loses its connection for a while, Matinee keeps trying for a few minutes and picks up where it was. Downloads play without the server at all.

<!-- since: v0.1.7 -->
### With no signal

Where the car has no connection, the lists you've opened on this drive show only what's downloaded, marked with a downloaded icon, so everything you see plays. When the signal comes back, the rest is there again.
<!-- /since -->

## Something isn't working

- **"Open Matinee on your phone and sign in"**: the phone app isn't signed in, or it's using the Home account away from home. Sign in on the phone and try again.
- **"Can't reach the server"**: the car can't reach your server from where it is. Check that the server can be reached away from home, or play your downloads.
- **Matinee isn't in the car's list of apps**: update the app to the latest version from Google Play, and check that Android Auto allows apps from Google Play (it does unless you changed it).
