---
title: Siri and Google Assistant
description: Ask Siri or Google Assistant to play your music, audiobooks and playlists from Matinee, hands free, with the app closed.
since: v0.1.7
sidebar:
  order: 8
---

You can ask Siri on an iPhone, or Google Assistant on an Android phone, to play something from your Matinee server. Say **"on Matinee"** at the end, so the phone knows which app to use:

- "Hey Siri, play **Abbey Road** on Matinee"
- "Hey Google, play **Yesterday by the Beatles** on Matinee"
- "Hey Siri, play **The Hobbit audiobook** on Matinee"
- "Hey Google, play **my Road Trip playlist** on Matinee"
- "Hey Siri, **shuffle Queen** on Matinee"
- "Hey Siri, play **Matinee**": picks up whatever you were listening to, where you left off

It works for music, audiobooks and your playlists. Videos aren't played by voice.

This needs the iPhone or Android app version 1.0.18 or later, signed in to your server, and a server on version 0.1.7 or later. There's nothing to set up on the server.

## How it finds what you asked for

Matinee looks through your music and audiobooks for the best match:

- **A song, album, artist or book by name.** When an album and a song have the same name, as with "Thriller", you get the album. Say "the song Thriller" for the song.
- **"… by someone"** narrows it to that artist, or for a book, its author.
- **A playlist** when you say the word "playlist", or when nothing else has that name.
- **A song** plays its album from that song, so the music keeps going.
- **A book** picks up where you left off, on any device.

Each person gets their own: a kids profile only finds what it's allowed to play, and playlists are your own.

## With the app closed

You don't need to open Matinee first. The phone finds your server on its own, at home or away (see [Watching away from home](/docs/server/remote-access/)), and plays in the background. It works from the lock screen, with AirPods or other headphones, and in the car with [CarPlay](/docs/apps/carplay/) or [Android Auto](/docs/apps/android-auto/).

If the Matinee app is open, what you asked for plays in it, so you see it on the screen.

Once something is playing, you can say "pause", "skip", "next song" or "what's playing?" to Siri or Google Assistant as with any music.

## Something isn't working

- **Siri says Matinee needs you to sign in**: the iPhone app isn't signed in, or it's using the Home account away from home. Sign in on the iPhone.
- **Siri or Google can't find something you know is there**: try the name the way it's written in Matinee, or add who it's by. Saying "album", "song" or "audiobook" also helps.
- **Google Assistant opens another app**: say "on Matinee" at the end. On Android, you can also make Matinee the music service Google Assistant uses (in the Google app's Assistant settings, under **Music**), if it's listed there.
- **Siri doesn't know Matinee**: update the app to the latest version from the App Store, then open it once.
