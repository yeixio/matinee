---
title: CarPlay
description: Play your music and audiobooks in the car with CarPlay, from the car's screen or its buttons, picking up where your iPhone left off.
since: v0.1.7
sidebar:
  order: 5
---

With the iPhone app, Matinee shows up in CarPlay with your music and audiobooks. You can browse and play from the car's screen, and use the car's buttons or the steering wheel to pause and skip. Videos never play in the car.

This needs the iPhone app version 1.0.16 or later, signed in to your server. There's nothing to set up on the server.

## Getting started

1. Sign in to your server in the Matinee app on your iPhone. The car uses the same server and the same account, or the profile you picked. Signing in again isn't needed for the car.
2. Connect your iPhone to the car, by cable or wirelessly.
3. Tap **Matinee** on the CarPlay home screen.

Your iPhone can stay in your pocket with the app closed: CarPlay starts Matinee by itself, and finds your server on its own, at home or away (see [Watching away from home](/docs/server/remote-access/)). If you watch at home without signing in, as the shared Home account, sign in on the iPhone first: the Home account only works on your home network.

## What's in the car

Matinee has up to four tabs along the top of the car's screen:

- **Listen now**: the books you're partway through, then the albums you've played lately.
- **Music**: **Shuffle all**, your **Playlists**, every **Artist** and **Album**, and the albums added lately. Pick an album to see its songs; pick a song to play the album from there. Pick a playlist to play it from the top. Playlists need app version 1.0.19 or later.
- **Audiobooks**: every book, with how far along you are in each.
- **Downloads**: the songs and books downloaded to your iPhone (see [Downloads](/docs/apps/downloads/)). These play with no connection, so they still work where the signal drops. The tab shows up once something is downloaded.

Picking something opens CarPlay's **Now Playing** screen, with the cover, the time and the buttons. Tap **Now Playing** on the CarPlay home screen or in Matinee to get back to it.

## Listening to a book

A book picks up exactly where you left off, on your iPhone, the web or a TV. For books, the car's buttons go **back 15 seconds** and **ahead 30 seconds**, and the **speed** button on the Now Playing screen steps through the speeds from 0.75× to 3×. It's the same speed the iPhone app uses for your books.

Your place is saved every few seconds, so it's there when you open the book again at home.

## The iPhone and the car together

The car and the iPhone share one player. If music is playing on the iPhone when you get in, the car shows it and its buttons control it. With the app open on the iPhone, what you pick in the car plays there too, so the iPhone shows the same song or book.

If the server restarts, or the car loses its connection for a while, Matinee keeps trying for a few minutes and picks up where it was. Downloads play without the server at all.

### With no signal

Where the car has no connection, Matinee still shows the lists you've opened on this drive, with what needs your server greyed out. Your downloads stay playable. When the signal comes back, the lists fill in again by themselves.

## Something isn't working

- **"Open Matinee on your phone and sign in"**: the iPhone app isn't signed in, or it's using the Home account away from home. Sign in on the iPhone; the car picks it up by itself.
- **"Can't reach the server"**: the car can't reach your server from where it is. Tap **Try again**, check that the server can be reached away from home, or play your downloads.
- **Matinee isn't on the CarPlay home screen**: update the app to the latest version from the App Store. On the iPhone, open **Settings → General → CarPlay**, pick your car and check that Matinee isn't hidden.
