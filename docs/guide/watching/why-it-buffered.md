---
title: Why it buffered
description: When a video stops to buffer, Matinee works out why and says so in plain words, with a connection test and tips for admins.
since: v0.2.0
sidebar:
  order: 6
---

When a video stops to load for more than a moment, the player asks your server why. The server compares what the player measured with what it knows itself, and answers in plain words.

## What you see

If the cause is clear, a short note shows at the top of the picture for a few seconds, such as *The connection gets 3.2 Mbps, and this video needs 8.0 Mbps.* A gauge button also appears in the player's controls. Open it any time in the next few minutes to read the cause again, why the server converts this video (if it does), and to test your connection.

When nothing looked slow, no note pops up. The gauge says that the server and the connection looked fine, which usually means Wi-Fi or the network dropped for a moment.

## The causes

- **The server reads the file slowly.** Reading a piece of the file took the server two seconds or more, or it reads slower than the video plays. This is usually a busy network share or a sleeping disk.
- **The server converts slower than the video plays.** The server is converting this video (for example to a format the device plays, or to draw in subtitles) at less than real time.
- **The connection is slower than the video.** Pieces arrived slower than the video needs. Away from home, your home connection's upload speed is often the limit.
<!-- since: v0.2.4 -->
- **The server ran out of room.** The server's transcode folder filled up while converting this video. Admins can give it more room or free up its disk ([more](/docs/server/transcoding/#when-the-transcode-folder-runs-short-of-room)).
<!-- /since -->
- **Nothing found.** The server and the connection both looked fine.

## Test connection

**Test connection** in the gauge downloads from your server for a few seconds and shows how fast that was and how long a round trip takes. It sends noise that nothing on the way can compress, so the number is what your device really gets from the server.

## Tips for admins

Admins also see what they could change: a GPU for transcoding, a lower quality, [adaptive quality](/docs/server/streaming-quality/) so players can step down, or a look at the disk or share in [Server health](/docs/server/server-health/). Everyone else sees only the cause.

The latest cause for each stream also shows under **Playing now** in Server health.


## In the phone and TV apps

The apps ask the same way and show the same note. Once playback has buffered, a pulse button shows above the video on a phone, and among the player's buttons on a TV, with the cause and **Test connection**. To test the connection any time, open **Settings → Test connection**.

On a phone or TV, the app measures the connection with a short download from your server when it stalls. Reports and tests only go to your own server.
