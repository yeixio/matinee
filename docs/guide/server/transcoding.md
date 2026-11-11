---
title: Hardware transcoding
description: Let Matinee convert video on the GPU, which backends it supports, and how to give it a GPU in Docker, Kubernetes and packages.
sidebar:
  order: 3
---

When a device can't play a file as it is, Matinee converts it while you watch. A GPU makes that much faster and lighter on the server: a 4K HEVC film becomes 1080p H.264 without keeping the CPU busy.

At startup Matinee tests what the machine's FFmpeg can use, with a one-second test encode, and picks the best that works:

| Hardware | Backend |
| --- | --- |
| NVIDIA | NVENC to encode, NVDEC to decode |
| AMD or Intel on Linux | VAAPI |
| Intel | Quick Sync |
| Mac | VideoToolbox |
| AMD on Windows | AMF |
| Raspberry Pi and other ARM boards | V4L2 |

**Settings → Streaming → Transcoding** shows which one is in use, and lets an admin pick another or turn it off.

![The Transcoding settings, using the CPU on a server without a GPU](../images/transcoding.webp)

<!-- since: v0.1.5 -->
The same section turns on [adaptive quality](/docs/server/streaming-quality/), which makes several qualities at once so playback steps down on a slow connection.

<!-- /since -->
If the GPU can't handle a file (an old codec, a profile it doesn't decode), that playback starts again on the CPU, and the file keeps using the CPU from then on, so nobody sees an error.

<!-- since: v0.2.0 -->
## Repackaging instead of converting

Often a device plays the picture fine and only the rest of the file is the problem: an MKV on an iPhone, or a film whose sound is TrueHD or DTS. Then Matinee doesn't convert the picture at all. It repackages it as it is and converts only the sound, which takes almost no work and keeps the picture exactly as good as the file.

The apps and the web player tell the server which video they decode (H.264, HEVC, AV1) and whether the screen shows HDR. A picture they decode is copied, in 4K and HDR if that's what the file has, as long as:

- the screen shows the file's HDR (a screen without HDR gets it converted; HDR10+ plays as HDR10, and Dolby Vision as the HDR10 or HLG under it when the file has one),
- no picture subtitle is drawn into the video, and
- the quality is **Auto** or **Original** and within any cap for watching away from home.

Until [media analysis](/docs/server/media-analysis/) has looked at a new HEVC or AV1 file, Matinee doesn't know whether it's HDR, so it only repackages it for an HDR screen. With **Auto**, playback moves to the adaptive stream if the connection can't keep up with the original.

**Settings → Server health** shows each stream as **Repackaging video** or **Converting video**, and for a conversion, why: the device can't play the video format, the screen can't show the HDR, or a lower quality was picked.

<!-- /since -->

<!-- since: v0.2.0 -->
## HDR on screens without it

Converted video is always SDR, so when Matinee converts an HDR film (HDR10, HDR10+, HLG or Dolby Vision) it tone maps it: the bright HDR picture is brought down to what an SDR screen shows, instead of coming out flat and grey. A screen that shows HDR gets the HDR picture as it is whenever it can, [repackaged](#repackaging-instead-of-converting) rather than converted.

**Settings → Streaming → Transcoding → HDR tone mapping** picks how: **Hable** (the default) keeps highlights natural, **Mobius** keeps more of the brightness, and **Reinhard** gives softer highlights. **Off** turns it off.

- Dolby Vision profiles 7 and 8 are tone mapped from the HDR10 or HLG picture they carry.
- Dolby Vision profile 5 has no such picture, and needs FFmpeg's libplacebo with a Vulkan device (a GPU, or Mesa's software one). Without it, those files convert without tone mapping.
- Tone mapping needs an FFmpeg built with zimg. The Docker image and the Linux packages have one. Homebrew's FFmpeg doesn't, so a server installed with `brew` converts HDR films without tone mapping. The setting says so when the server's FFmpeg can't.
<!-- /since -->
<!-- since: v0.3.0 -->
- Matinee Server, the Mac app, tone maps too: the FFmpeg inside it now includes zimg.
<!-- /since -->
<!-- since: v0.2.0 -->
- With a GPU, the picture is still decoded and resized on the GPU, and tone mapped on the CPU at the smaller size, which is light work.

**Settings → Server health** marks a stream **HDR made SDR** while it's tone mapped.

<!-- /since -->

<!-- since: v0.2.2 -->
## Dolby Vision

Dolby Vision files come in a few kinds (profiles), and each plays the best way the screen allows. The apps on iPhone, iPad, Apple TV and Android, and browsers that decode it (such as Safari on a Mac with a Dolby Vision screen), tell the server when the screen shows Dolby Vision.

| The file | Dolby Vision screen | HDR10 screen | Screen without HDR |
| --- | --- | --- | --- |
| Profile 8.1 (most streaming rips) | Dolby Vision, repackaged | Its HDR10 picture | Tone mapped |
| Profile 8.4 (HLG under it) | Dolby Vision, repackaged | Its HLG picture, if the screen shows HLG | Tone mapped |
| Profile 7 (from Blu-ray discs) | Its HDR10 picture | Its HDR10 picture | Tone mapped |
| Profile 5 (nothing under it) | Dolby Vision, repackaged | Converted and tone mapped | Tone mapped |

- Profile 5 is never sent as it is to a screen without Dolby Vision, where its colours would come out purple and green. From version 1.0.28 the apps don't play such a file straight from the server either, and Android counts a screen as Dolby Vision only when the device also has a Dolby Vision decoder.
- Profile 7 has a second layer almost no player decodes, so it always plays as the HDR10 picture it carries.
- When a screen gets the HDR10 or HLG picture, the server takes the Dolby Vision data out so no player mistakes it for Dolby Vision it can't show. That needs FFmpeg 7.1 or later, which the Docker image has; with an older FFmpeg the data stays in, and players that don't show Dolby Vision ignore it.

<!-- /since -->
<!-- since: v0.2.4 -->
## When the transcode folder runs short of room

A transcode writes the video into the transcode folder as it plays, and a repackaged 4K remux takes as much room as its file: a 100 GB film needs 100 GB. Before FFmpeg starts, Matinee works out how much the rest of the video will take and compares it with the room the folder has.

- **It fits.** The transcode runs as usual, and seeking back anywhere is instant.
- **It doesn't fit.** The transcode saves space: it keeps only the minutes around where the player is. Pieces more than two minutes behind are deleted, and FFmpeg waits once it's three minutes ahead, going on as the player catches up. Any length of film plays in a few gigabytes. Seeking further back than what's kept starts the transcode again from there, which takes a moment longer.
- **Not even a few minutes fit.** The video doesn't start, and the player says *The server is out of room to convert this video* instead of failing without a reason.

The room is checked every few seconds while transcodes run. When something else fills the disk, or several people start long films at once, the running transcodes start saving space before the disk is full. If the disk still fills up, that transcode stops, the player says the server ran out of room, and its [Why it buffered](/docs/watching/why-it-buffered/) note says so too.

Admins see the room left under **Transcodes** in [Server health](/docs/server/server-health/), with a warning while transcodes are saving space, and get the **Transcodes run out of room** [alert](/docs/server/alerts/).

Matinee keeps 512 MB free on the disk for everything else, such as the database. When the folder's volume has a size limit smaller than its disk, as a Kubernetes `emptyDir` does, set `MATINEE_CACHE_LIMIT` to it so Matinee keeps within it ([configuration](/docs/server/configuration/)). The [Helm chart](/docs/server/kubernetes/) does this for you.
<!-- /since -->

## Giving Matinee a GPU

**Docker:** pass `/dev/dri` in for AMD and Intel, and add the group that owns it on the host:

```yaml
services:
  matinee:
    image: ghcr.io/yeixio/matinee:latest
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - "993"   # the render group's number on the host: getent group render
```

For NVIDIA, install the NVIDIA Container Toolkit on the host and request a GPU (`--gpus all`, or a `deploy.resources` reservation in Compose; the [`docker-compose.yml`](https://github.com/yeixio/matinee/blob/main/docker-compose.yml) has both examples). The image includes FFmpeg with every Linux backend and the AMD and Intel drivers.

**Kubernetes:** set `gpu.type` to `vaapi` or `nvidia`, and `nodeSelector` to keep Matinee on the node with the GPU. For VAAPI, list the node's render group in `gpu.vaapi.groups`; if it's wrong, **Settings → Streaming → Transcoding** says which number it needs. See `gpu:` in the chart's [`values.yaml`](https://github.com/yeixio/matinee/blob/main/deploy/helm/matinee/values.yaml).

**Packages:** Matinee uses the system's FFmpeg and drivers, and its user is already in the `render` and `video` groups.

On a machine with several GPUs, `MATINEE_VAAPI_DEVICE` picks one for VAAPI, such as `/dev/dri/renderD129`.

:::note[Help test it]
VAAPI on AMD has been tried on real hardware so far. If you run one of the others, a [compatibility report](https://github.com/yeixio/matinee/issues/new?template=compatibility_report.yml) helps a lot.
:::
