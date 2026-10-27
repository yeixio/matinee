---
title: Install Matinee
description: Run Matinee with Docker, a Debian, Ubuntu or Fedora package, Homebrew, Helm on Kubernetes, or from source.
sidebar:
  order: 1
---

Matinee runs on Linux and macOS, on amd64 and arm64 (and armv7 for older NAS boxes and Raspberry Pis). On Windows, run it in Docker. Pick whichever way of installing suits the machine. Every option ends the same way: open `http://<your server>:7171` in a browser and [finish setup](/docs/getting-started/first-run/).

## Docker

```sh
docker run -d --name matinee -p 7171:7171 \
  -v ./data:/data \
  -v /path/to/movies:/media/movies:ro \
  -v /path/to/tv:/media/tv:ro \
  -v /path/to/music:/media/music:ro \
  ghcr.io/yeixio/matinee:latest
```

- `/data` holds Matinee's database, its image cache and scratch space for converting video. Keep it on a local disk.
- Mount each media folder read-only under `/media`. You pick these folders in Matinee later.
- The image includes FFmpeg with every Linux hardware backend. To use a GPU, see [Hardware transcoding](/docs/server/transcoding/).

There's a [`docker-compose.yml`](https://github.com/yeixio/matinee/blob/main/docker-compose.yml) in the repository if you prefer Compose.

:::tip[Let the apps find the server]
A container on Docker's default bridge network can't announce itself on your home network, so the phone and TV apps won't list it by themselves. Run it with `--network host` (instead of `-p 7171:7171`) and they will. See [Your home network](/docs/server/home-network/).
:::

## Debian and Ubuntu

```sh
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://raw.githubusercontent.com/yeixio/matinee/packages/matinee.asc | sudo tee /etc/apt/keyrings/matinee.asc > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/matinee.asc] https://raw.githubusercontent.com/yeixio/matinee/packages/apt stable main" | sudo tee /etc/apt/sources.list.d/matinee.list
sudo apt-get update && sudo apt-get install matinee
```

## Fedora, RHEL and friends

```sh
sudo curl -fsSL https://raw.githubusercontent.com/yeixio/matinee/packages/matinee.repo -o /etc/yum.repos.d/matinee.repo
sudo dnf install matinee
```

Fedora's own `ffmpeg-free` satisfies the FFmpeg dependency. On RHEL, Rocky and Alma, enable [EPEL and RPM Fusion](https://rpmfusion.org/Configuration) first.

### What the packages set up

Both packages (amd64 and arm64, plus armhf for apt) install a `matinee` service that starts right away and on every boot.

| | |
| --- | --- |
| Runs as | the `matinee` user, which is also in the `render` and `video` groups for hardware transcoding |
| Data | `/var/lib/matinee` |
| Settings | `/etc/matinee/matinee.env`, then `sudo systemctl restart matinee` |
| Logs | `journalctl -u matinee` |

If the `matinee` user can't read a network share, mount the share with that user's permissions or add the user to the share's group.

The repositories and packages are signed with the key "Matinee packages &lt;hello@yeix.io&gt;", fingerprint `4EE3 3F8C EE07 6BB0 C9A7  C559 21F5 BE9F 726A 2895`.

## macOS and Linux with Homebrew

```sh
brew tap yeixio/matinee https://github.com/yeixio/matinee
brew install yeixio/matinee/matinee
brew services start matinee
```

Then open http://localhost:7171. Settings go in `$(brew --prefix)/etc/matinee/matinee.env` (then `brew services restart matinee`), and data lives in `$(brew --prefix)/var/matinee`.

## Kubernetes

```sh
helm install matinee oci://ghcr.io/yeixio/charts/matinee -f my-values.yaml
```

The chart bundles PostgreSQL and generates its password on first install, so there's nothing to set up and it works the same under Argo CD or Flux. Mount your media read-only through `media:` in your values. [Kubernetes](/docs/server/kubernetes/) has the details.

## From source

You need Go 1.26 or newer, Node 22 with pnpm, and FFmpeg.

```sh
git clone https://github.com/yeixio/matinee
cd matinee
make run    # builds the web player and the server, then serves on :7171
```

## Next

[Create the admin account and add your libraries](/docs/getting-started/first-run/).
