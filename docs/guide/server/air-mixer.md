---
title: Air-Mixer
description: Play your music on every speaker in the house with Air-Mixer, and use a TV or a phone with Matinee open as one of Air-Mixer's speakers.
since: v0.2.3
sidebar:
  order: 9
---

[Air-Mixer](https://github.com/yeixio/air-mixer) is a small self-hosted server that puts every speaker you own behind one app: AirPlay speakers, the computer it runs on, and more. Connect it to Matinee and the two work together both ways:

- **Matinee plays on Air-Mixer's speakers.** Each Matinee input you add in Air-Mixer shows up in Matinee as a player, like a TV or a phone, and in [Home Assistant](/docs/server/home-assistant/) as a media player. Start an album on it and Air-Mixer plays it on whichever speakers its routes send it to: the kitchen, the patio, or all of them at once.
- **Matinee players become Air-Mixer speakers.** A TV, phone or browser with Matinee open can be one of Air-Mixer's outputs. Anything Air-Mixer plays, such as an iPhone playing to it over AirPlay, comes out of that TV too.

## Setting it up

You need a Matinee admin account and Air-Mixer on the same network as Matinee.

1. **Point Air-Mixer at Matinee.** In Air-Mixer's web page, scroll to **Matinee**, type the address you open Matinee at, such as `http://192.168.1.20:7171`, and click **Save**.
2. **Sign in.** Click **Sign in**. Air-Mixer shows a code and a link to your server's **/link** page. Open it as an admin, check that it says **Air-Mixer**, and click **Sign in**. Air-Mixer carries on by itself, and appears under **Settings → You → Your devices**, where you can sign it out.

## Playing Matinee on Air-Mixer's speakers

1. In Air-Mixer, add an **input server** with the protocol **Matinee** and a name, such as _Whole house_.
2. Add a **route** from it to the speakers you want.
3. In Matinee, the new player is there with the others. Start music on it from Home Assistant, or from anything else that controls Matinee players.

The player takes play, pause, seek, stop, previous, next and volume. A song goes on to the next song of its album when it ends, and Matinee remembers where you were in audiobooks and podcasts, as on any other player.

Matinee sends Air-Mixer the audio ready to play, so every file Matinee plays works, whatever its format: FLAC, MP3, AAC, Opus, a podcast episode or a radio station.

## Using a Matinee player as a speaker

1. Open Matinee on the TV, phone or browser you want to use. It has to be open to show up.
2. In Air-Mixer, click **Add Client**, then **Discover Devices**. Matinee players are listed with the AirPlay speakers. Pick one and click **Add Client**.
3. Add it to a route.

Air-Mixer adds a radio station named **Air-Mixer · _the speaker's name_** to your first music library and starts it on the player. You can play that station yourself on any player too. If the TV is switched off, Air-Mixer starts the station again when the TV comes back with Matinee open.

A few things to know:

- **There's a delay.** Players buffer a few seconds of a station, so a Matinee player plays a little after Air-Mixer's AirPlay speakers. It's fine for background music, not for a party in two rooms at once.
- **Matinee has to reach Air-Mixer.** Matinee fetches the station from Air-Mixer's web port (9001). Air-Mixer gives Matinee its own address on your network; if that's wrong, for example when Air-Mixer runs in Docker or Kubernetes, set `AIRMIXER_SERVER_PUBLIC_URL` to the address Matinee should use, such as `http://192.168.1.10:9001`.
- **Only admins** can add a station on the home network. Air-Mixer is signed in as the admin who approved it.

## For developers

Air-Mixer uses Matinee's public API, so anything else can do the same:

- `GET /api/items/{id}/audio?startMs=` sends an item's audio as an endless WAV of 16-bit stereo at 44.1 kHz, for speakers that play what they're sent without decoding it.
- `POST /api/stations` with `"home": true` lets an admin add a station on the home network. Matinee otherwise only fetches stations from the internet. A station that streams WAV plays to players as AAC.
- Players come and go through `/api/clients`, as described in [Remote control](https://github.com/yeixio/matinee/blob/main/docs/remote-control.md).
