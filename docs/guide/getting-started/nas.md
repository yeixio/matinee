---
title: Install on a NAS
description: Run Matinee on Unraid, TrueNAS SCALE or ZimaOS, with your media shares mounted read-only and the GPU passed in.
sidebar:
  order: 1
---

Matinee runs as one container, so any NAS that runs Docker can run it. The steps below are for the systems with an app screen. Whichever you use, the pattern is the same:

- Keep `/data` (the database, image cache and scratch space for converting video) on fast local storage, such as an SSD pool or cache drive.
- Mount your media read-only. You pick the folders in Matinee when you [add a library](/docs/getting-started/first-run/).
- Matinee needs to read your media files. If it can't see a folder's contents, check the user the container runs as can read the share.

When it's running, open `http://<your NAS>:7171` (or the port you chose) and [finish setup](/docs/getting-started/first-run/).

## Unraid

Matinee has an Unraid template. Until it's listed in Community Applications, add it by hand:

1. Go to **Docker**, and at the bottom of the page set **Template repositories** to include `https://github.com/yeixio/matinee`, then **Save**.
2. Click **Add Container** and pick **Matinee** from the **Template** list.
3. Check the paths: **Appdata** goes to `/mnt/user/appdata/matinee`, and **Media** mounts `/mnt/user/` read-only at `/media`. Point it at a single share instead if you'd like.
4. Click **Apply**.

The template uses host networking, so the phone and TV apps find the server by themselves. It passes `/dev/dri` in for Intel and AMD graphics; remove that entry if your server has no GPU. For NVIDIA with the Nvidia Driver plugin, add `--runtime=nvidia` to **Extra Parameters** and set **NVIDIA_VISIBLE_DEVICES**.

The container runs as root so it can write to the appdata folder Unraid creates and use the GPU without extra groups.

## TrueNAS SCALE

Install Matinee as a custom app from YAML:

1. Go to **Apps → Discover Apps → Custom App**, and choose **Install via YAML**.
2. Paste this, changing the dataset paths to yours:

```yaml
services:
  matinee:
    image: ghcr.io/yeixio/matinee:latest
    user: "568:568"   # TrueNAS's apps user
    ports:
      - "7171:7171"
    environment:
      MATINEE_MEDIA_ROOT: /media
    volumes:
      - /mnt/tank/apps/matinee:/data
      - /mnt/tank/media:/media:ro
    restart: unless-stopped
```

3. Create the `/mnt/tank/apps/matinee` dataset first, and give the `apps` user (568) permission to write to it and read your media dataset.

For hardware transcoding, add `devices: ["/dev/dri:/dev/dri"]` and a `group_add` with your render group's number, as in [Hardware transcoding](/docs/server/transcoding/).

## ZimaOS and CasaOS

1. Open the **App Store**, click **Custom Install** (the **+** at the top right), then **Import**.
2. Paste the Compose file from [`packaging/zimaos`](https://github.com/yeixio/matinee/blob/main/packaging/zimaos/Matinee/docker-compose.yml) in the repository and **Submit**.

It mounts your **Media** folder (`/DATA/Media`) read-only at `/media`, keeps Matinee's data under `/DATA/AppData/matinee`, and passes in the GPU.

## Other NAS systems

On Synology (Container Manager), QNAP (Container Station) and others, create a project from the [`docker-compose.yml`](https://github.com/yeixio/matinee/blob/main/docker-compose.yml) in the repository and change the media paths to your shares. See [Install Matinee](/docs/getting-started/install/#docker) for what each mount is for.
