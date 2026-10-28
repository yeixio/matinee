---
title: Casting to a TV
description: Send what you're watching from the phone app to a TV with AirPlay or Chromecast, and pick it up on the phone again.
since: v0.1.1
sidebar:
  order: 4
---

The phone and tablet apps can send a movie or episode to a TV that has no Matinee app of its own: an Apple TV or AirPlay TV from an iPhone or iPad, and a Chromecast or a TV with Chromecast built in from any phone. The TV plays it straight from your server, in full quality, while the phone becomes the remote.

## AirPlay

On an iPhone or iPad, start playing, then tap the **AirPlay** button at the top of the player, next to the title, and pick the TV. The phone shows **Playing on your TV with AirPlay**, and its controls keep working: pause, skip and the bar along the bottom.

AirPlay works with an Apple TV and with TVs that support it, including many recent Samsung, LG, Sony and Vizio sets. The phone and the TV need to be on the same network.

## Chromecast

When there's a Chromecast on your network, the player shows the **Cast** button at the top, next to the title. Tap it and pick the TV. The phone shows **Playing on** and the Chromecast's name. Tap **Show controls** to pause, skip or change the volume.

Chromecast works with Chromecast devices, Google TV Streamer, and TVs with Chromecast built in, such as many Sony, TCL, Philips, Toshiba and Vizio sets.

- **Where you left off:** the phone keeps saving your place while the TV plays, so Continue watching on any device picks up from there.
- **Up next** counts down on the phone at the end of an episode, then sends the next one to the TV.
- **Stop casting** with the Cast button. Playback comes back to the phone where the TV left off.

Most files play on the Chromecast as they are. Anything it can't play (MKV files, HEVC, surround audio) is converted by the server as it plays, the same way as for a browser. See [Transcoding](/docs/server/transcoding/) for what that takes.

Casting isn't offered on Fire tablets, which have no Google Cast, or in the TV apps.

## When it doesn't work

- **No Cast or AirPlay button:** the phone and the TV must be on the same network, and some routers keep devices on guest Wi-Fi apart. On an iPhone, check that Matinee has **Local Network** turned on in Settings → Privacy & Security.
- **The TV says it can't play the video:** the TV loads the video from the server itself, at the address the phone uses. If the phone reaches the server through an outside address that only works away from home, check that the TV can reach it too.
