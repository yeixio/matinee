# Matinee

A fast, self-hosted media server for your movies, TV and music. Point it at your folders and it does the rest: tidy libraries, artwork and details, and smooth playback on any screen. No ads, and no account on someone else's server.

![Home](docs/screenshots/desktop-home.png)

<p>
  <img src="docs/screenshots/phone-home.png" width="195" alt="Home on a phone">
  <img src="docs/screenshots/phone-season.png" width="195" alt="A season on a phone">
  <img src="docs/screenshots/desktop-album-playing.png" width="560" alt="An album playing">
</p>

**Status:** early. The server and web player work end to end; native apps come later.

## What it does

- **Libraries** for movies, TV and music. Point it at folders; it names things from the file and folder names (`Movie (2021)`, `Show/Season 1/Show - S01E02.mkv`, `Artist/Album/01 Track.flac`), reads music tags, and picks up `poster.jpg`, `fanart.jpg` and `cover.jpg` next to your files.
- **New files show up in seconds.** Matinee watches library folders and adds what you copy in, rename or delete a few seconds after the copy finishes, scanning just that file or folder. Folders on this machine's own disks announce changes as they happen; network shares (SMB, NFS) don't, so Matinee checks those every 30 seconds by looking at each folder's modification time, which is cheap even on a big library. A full rescan every 15 minutes catches the rest. Settings → New files shows how each library is watched.
- **Plays anywhere.** Files the browser can play stream as-is, with instant seeking. Anything else is converted on the fly with FFmpeg to HLS. H.264 video is repackaged, not re-encoded, so it costs almost no CPU.
- **Subtitles** from the file itself or from files next to it (`Movie.en.srt`, `Movie.en.forced.srt`, `.vtt`, `.ass`). Text subtitles are drawn by the player; Blu-ray and DVD picture subtitles are burned into the video while converting. The player remembers your language from one episode to the next.
- **Picks up where you left off.** Resume points, watched marks, Continue Watching and Recently Added, per person.
- **Collections.** Smart collections fill themselves from rules, using the same filters as the library pages ("unwatched 80s action", "kids movies under 90 minutes"), and follow each person's own watched state. Keep collections by hand too, find box sets like a trilogy filled in from TMDB, and pin any collection to Home or a library page as a row, sorted by any field or shuffled daily. New servers start with Recently added, Unwatched and Short enough for tonight.
- **Music** with a player that keeps going while you browse.
- **Knows what's in every file.** In the background, Matinee slowly reads each file for its resolution, HDR (HDR10, HDR10+, HLG, Dolby Vision), codecs and bitrates, audio channels, Atmos and languages, and subtitles. The essentials show as badges on each title and as library filters (4K, HDR, Dolby Atmos…); everything else is under File details. It reads one file at a time at the lowest priority, waits while someone's watching something being converted, and looks at a file again only when it changes, so network shares stay responsive. Admins can see its progress, or slow it down or pause it, under Settings.
- **Posters and descriptions** from [TMDB](https://www.themoviedb.org/) with a free API key ([how to get one](#posters-and-descriptions-tmdb-key)). Your own artwork files always win, and everything else works without a key.
- **Search** as you type.
- **People:** an admin plus as many accounts as you like, each with their own history.
- **Profiles and kids mode:** one sign-in, or the Home account on your network, can have profiles that each keep their own progress, watch list and theme, with "Who's watching?" when the app opens and an optional 4-digit PIN. Kids profiles see only titles rated for them (Little kids, Kids or Teens, from TMDB's age ratings for your country) in the libraries you pick, and can't open settings or make requests. Admins set them up under Settings → Profiles.
- **Downloads** for watching offline in the Matinee apps. A download is a job on the server, not a stream: it's prepared in the background (the original file, or a single MP4 at 1080p, 720p or 480p), checksummed, then fetched by the phone at its own pace, resuming where it broke off. Jobs survive restarts and retry on their own, and watch progress made offline is merged back when the phone reconnects, with the latest change winning.
- **Radarr and Sonarr** (optional): people ask for movies and shows and an admin approves them, everyone sees what's coming soon, and anyone can report a problem with what they're watching. Admins can change a title's quality or have a bad file replaced. [Setting it up](#radarr-and-sonarr).
- **Themes:** seven built in (Matinee, Dark, Light, Clean, Sci-fi, Drive-in, High contrast), picked by each person under Settings → Appearance and followed by the web player and the phone and TV apps. Add your own with a small JSON file; see [docs/themes.md](docs/themes.md).
- **Devices:** sign in a phone or TV by typing the code it shows at `/link`, and see or sign out every device you're signed in on under Settings.

## Run it

### Docker

```sh
docker run -d --name matinee -p 7171:7171 \
  -v ./data:/data \
  -v /path/to/movies:/media/movies:ro \
  -v /path/to/tv:/media/tv:ro \
  -v /path/to/music:/media/music:ro \
  ghcr.io/yeixio/matinee:latest
```

Open http://localhost:7171, create the admin account, and add your folders under Settings. A [`docker-compose.yml`](docker-compose.yml) is included too.

### Kubernetes

```sh
helm install matinee deploy/helm/matinee -f my-values.yaml
```

The chart runs Matinee with a bundled PostgreSQL, so nothing depends on file locking over network storage. Its credentials are generated into a Secret on first install, so there's nothing to set up, and this works the same under Argo CD or Flux. Mount your media read-only through `media:` in the values (NFS, a PVC, anything); transcodes and the image cache use a local `emptyDir`. To use your own PostgreSQL, set `postgresql.enabled=false` and `database.url` (or `database.existingSecret`). See [`values.yaml`](deploy/helm/matinee/values.yaml).

Run one replica for now: an active transcode lives in the pod that started it. More replicas work with `service.sessionAffinity: ClientIP`.

### Following the latest build

To try each change as it lands, point Argo CD at the `edge` branch. CI moves `edge` to a commit on `main` only once its checks pass and its image is published, so the cluster never waits on an image that isn't there yet. The chart from that commit runs the image built from it (`sha-<commit>`), and **Settings** shows the same commit as `main-<commit>`.

```yaml
source:
  repoURL: https://github.com/yeixio/matinee.git
  targetRevision: edge
  path: deploy/helm/matinee
  helm:
    parameters:
      - name: image.tag
        value: sha-$ARGOCD_APP_REVISION_SHORT
```

Argo CD checks the branch every few minutes, so a new build rolls out shortly after CI finishes. For releases instead, install the chart from `oci://ghcr.io/yeixio/charts/matinee` with a version.

### Finding it from the apps

The Matinee apps list servers on your home network by themselves, so nobody has to know an IP address. The server announces itself over multicast DNS (Bonjour), and the apps show it on their first screen.

- **apt, dnf, Homebrew and source installs** are found as they are.
- **Docker:** a container on the default bridge network can't announce itself. Run it with `--network host` instead of `-p 7171:7171`, or keep typing the address.
- **Kubernetes:** the chart runs a small announcer on a node's host network that points the apps at your ingress address. If your ingress is managed outside the chart, set `discovery.url` to the address you open Matinee at.

To keep a server unlisted, set `MATINEE_DISCOVERY=false` (`discovery.enabled=false` in the chart). The apps can always connect by address.

### Watching at home without signing in

Phones, TVs and browsers on your home network can browse and play without an account. They share one Home account, so they share its watch progress, and they can't change anything: no settings, no downloads, no approving other devices. Away from home, everyone signs in. To make everyone sign in at home too, turn on **Settings → Home network → Ask for sign-in at home**.

Matinee decides a device is at home by the address its connection comes from, and only when nothing sits in between:

- **apt, dnf, Homebrew, source and Docker with `--network host`:** a device with a private address (such as `192.168.x.x`) connecting straight to the server counts as home.
- **Behind a reverse proxy or ingress,** every request arrives from the proxy, the internet's included, so requests through it never count. Give the home network its own way in with `MATINEE_HOME_ADDR` (for example `:7172`). Then only that address counts, and the apps on your network use it. Don't forward that port from your router.
- **Kubernetes:** the chart does this for you. It opens port 7172 on the server's node (`discovery.homePort`) and runs the announcer on the same node, so the apps at home connect there and use your ingress address away from home. Set `discovery.homePort=0` to turn it off.

If something in front of Matinee rewrites addresses without saying so, for example a port forward that makes internet traffic look like it comes from your router, turn on **Ask for sign-in at home**.

### Debian and Ubuntu

```sh
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://raw.githubusercontent.com/yeixio/matinee/packages/matinee.asc | sudo tee /etc/apt/keyrings/matinee.asc > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/matinee.asc] https://raw.githubusercontent.com/yeixio/matinee/packages/apt stable main" | sudo tee /etc/apt/sources.list.d/matinee.list
sudo apt-get update && sudo apt-get install matinee
```

### Fedora, RHEL and friends

```sh
sudo curl -fsSL https://raw.githubusercontent.com/yeixio/matinee/packages/matinee.repo -o /etc/yum.repos.d/matinee.repo
sudo dnf install matinee
```

Fedora's own `ffmpeg-free` satisfies the FFmpeg dependency. On RHEL, Rocky and Alma, enable [EPEL and RPM Fusion](https://rpmfusion.org/Configuration) first for FFmpeg.

Both packages (amd64 and arm64, plus armhf for apt) install `matinee.service`, which starts right away and on boot as the `matinee` user, with its data in `/var/lib/matinee`. Settings go in `/etc/matinee/matinee.env`; run `sudo systemctl restart matinee` after a change. Open http://your-server:7171 to finish setup. The user is also added to the `render` and `video` groups for hardware transcoding. To read a share the `matinee` user can't, mount it with that user's permissions or add the user to the share's group.

### Homebrew (macOS and Linux)

```sh
brew tap yeixio/matinee https://github.com/yeixio/matinee
brew install yeixio/matinee/matinee
brew services start matinee
```

Then open http://localhost:7171. Settings go in `$(brew --prefix)/etc/matinee/matinee.env` (then `brew services restart matinee`), and data lives in `$(brew --prefix)/var/matinee`.

### Posters and descriptions (TMDB key)

Matinee looks up posters, backdrops and descriptions on [TMDB](https://www.themoviedb.org/). For now, each server uses its own free TMDB key. It takes about two minutes:

1. Sign up at [themoviedb.org](https://www.themoviedb.org/signup) and confirm your email.
2. Open [Settings → API](https://www.themoviedb.org/settings/api), click **Create**, and answer **Yes** to "personal use". Your own server is personal use.
3. Fill in the short form. For example, use "Matinee" as the name and "Self-hosted media server for my household" as the summary.
4. Copy the **API Key** (or the API Read Access Token; either works).
5. Give it to Matinee:
   - Docker: add `-e MATINEE_TMDB_KEY=your-key` to `docker run`.
   - Kubernetes: run `kubectl create secret generic matinee-tmdb --from-literal=tmdb-api-key=your-key`, then set `tmdb.existingSecret: matinee-tmdb` in your values.
   - apt or dnf: add `MATINEE_TMDB_KEY=your-key` to `/etc/matinee/matinee.env`.
   - Homebrew: add `MATINEE_TMDB_KEY=your-key` to `$(brew --prefix)/etc/matinee/matinee.env`.

Restart Matinee. The next library scan fills in the artwork.

**Why isn't a key built in?** TMDB's free keys are for personal use, and a key shipped inside Matinee would be shared by every server. Once donations or client app sales bring in enough, we'll license a TMDB key for the project and build it into the official images, so new servers get artwork with no setup. Until then, you use your own key.

### Radarr and Sonarr

If you use [Radarr](https://radarr.video/) for movies or [Sonarr](https://sonarr.tv/) for TV, connect them under **Settings → Radarr and Sonarr**:

1. Click **Connect**, enter the app's address (such as `http://radarr:7878`) and its API key (in the app under **Settings → General**), then **Test**.
2. Pick the quality and folder that new titles get, and **Save**.
3. Choose who can add titles: anyone can ask and an admin approves (the default), anyone's requests are added right away, or only admins add.

The address and key stay on the server. Then:

- **Requests** (the inbox icon) is where people find a movie or show and ask for it, and where admins approve requests and see reported problems. A problem with a movie or episode can be fixed with **Replace file**, which deletes the file in Radarr or Sonarr and has it download another copy.
- **Coming soon** (the calendar icon, and a row on Home) lists the next two months of new episodes and movie releases that aren't in yet.
- On a movie or show, admins can change its quality, and anyone signed in can **Report a problem**.

Devices at home that haven't signed in can see what's coming soon, but can't ask for titles or report problems.

### From source

Needs Go 1.26+, Node 22 with pnpm, and FFmpeg.

```sh
make run          # builds the web player and server, then serves on :7171
make demo         # optional: a small fake library in ./demo-media to try it with
```

## Configuration

Every setting is a flag or an environment variable.

| Variable | Default | What it does |
| --- | --- | --- |
| `MATINEE_ADDR` | `:7171` | Listen address |
| `MATINEE_DATA_DIR` | `./data` | SQLite database (when used), image cache, transcode scratch space |
| `MATINEE_DATABASE_URL` | empty | `postgres://…` to use PostgreSQL instead of SQLite |
| `MATINEE_TMDB_KEY` | empty | TMDB API key or read access token, for posters and descriptions ([get one](#posters-and-descriptions-tmdb-key)) |
| `MATINEE_RATING_COUNTRY` | `US` | Whose age ratings to use for kids profiles, such as `GB`, `CA`, `AU` or `DE`. Titles pick up a new country's ratings when their details are next refreshed |
| `MATINEE_MEDIA_ROOT` | `/` | Folder the library picker opens in |
| `MATINEE_MUSICBRAINZ` | `true` | Look up artists and albums on MusicBrainz, with covers from the Cover Art Archive and artist photos from Wikidata. No key needed. Set `false` to use only local artwork |
| `MATINEE_WIKIDATA` | `true` | Look up the awards movies and shows won or were nominated for on Wikidata. No key needed. Set `false` to skip awards |
| `MATINEE_SCAN_INTERVAL` | `15m` | How often libraries are rescanned in full; `0` turns it off |
| `MATINEE_WATCH_POLL_INTERVAL` | `30s` | How often folders on network shares are checked for new files. A big library on a slow share is checked less often by itself, so the share stays responsive |
| `MATINEE_DISCOVERY` | `true` | Announce the server on the local network so the apps find it ([more](#finding-it-from-the-apps)) |
| `MATINEE_SERVER_NAME` | `Matinee` | The name the apps show for this server |
| `MATINEE_PUBLIC_URL` | empty | The address the apps should connect to, such as `https://matinee.example.com`; empty uses this machine's IP and port |
| `MATINEE_HOME_ADDR` | empty | A second listen address just for the home network, such as `:7172`, when the main one is behind a proxy. Only devices that connect here can watch without signing in (see above) |
| `MATINEE_FFMPEG`, `MATINEE_FFPROBE` | `ffmpeg`, `ffprobe` | Paths to FFmpeg |
| `MATINEE_VAAPI_DEVICE` | empty | The GPU to use for VAAPI, such as `/dev/dri/renderD129`, on a machine with several; empty tries each |

### Hardware transcoding

When a device can't play a file as it is, Matinee converts it while you watch. A GPU makes that much faster and lighter on the server: a 4K HEVC film becomes 1080p H.264 without keeping the CPU busy. At startup Matinee tests what the machine's FFmpeg can use, with a one-second test encode, and picks the best that works:

| Hardware | Backend |
| --- | --- |
| NVIDIA | NVENC to encode, NVDEC to decode |
| AMD or Intel on Linux | VAAPI |
| Intel | Quick Sync |
| Mac | VideoToolbox |
| AMD on Windows | AMF |
| Raspberry Pi and other ARM boards | V4L2 |

**Settings → Transcoding** shows which one is in use, and lets an admin pick another or turn it off. If the GPU can't handle a file (an old codec, a profile it doesn't decode), that playback starts again on the CPU, and the file keeps using the CPU from then on, so nobody sees an error.

To give Matinee a GPU:

- **Docker:** pass `/dev/dri` in for AMD and Intel, and add the group that owns it on the host (see [`docker-compose.yml`](docker-compose.yml)). For NVIDIA, install the NVIDIA Container Toolkit and request a GPU. The image includes FFmpeg with every Linux backend and the AMD and Intel drivers.
- **Kubernetes:** set `gpu.type` to `vaapi` or `nvidia`, and `nodeSelector` to keep Matinee on the node with the GPU. See `gpu:` in [`values.yaml`](deploy/helm/matinee/values.yaml).
- **Packages:** Matinee uses the system's FFmpeg and drivers, and its user is already in the `render` and `video` groups.

Only VAAPI on an AMD card has been tried on real hardware so far. If you have one of the others, a [compatibility report](https://github.com/yeixio/matinee/issues/new?template=compatibility_report.yml) helps a lot.

**SQLite or PostgreSQL?** SQLite is the default and needs nothing else: right for a single machine or a NAS, as long as `MATINEE_DATA_DIR` is on a local disk. Use PostgreSQL on Kubernetes, with several servers, or whenever the data would otherwise live on network storage, where SQLite's locking isn't reliable.

## How it's built

One Go binary with the web player embedded. It talks to SQLite or PostgreSQL and runs FFmpeg for probing and transcoding.

```
cmd/matinee          entry point
internal/api         HTTP API, auth, serves the web player
internal/scan        library scanning; library kinds (movies, tv, music) register here
internal/analyze     background media analysis (HDR, codecs, audio, languages)
internal/metadata    online metadata providers (TMDB)
internal/artwork     poster and backdrop serving and caching
internal/stream      direct play and HLS transcoding
internal/hwaccel     finds the GPU and builds FFmpeg's arguments for it
internal/store       SQLite / PostgreSQL storage
web/                 React + Vite web player
deploy/helm/matinee  Helm chart
```

Library kinds and metadata providers are interfaces so plugins can add more later, such as an arcade (MAME) library.

The API is plain JSON over HTTP under `/api/v1`, open to any client, and documented in [`openapi.yaml`](internal/api/openapi.yaml) (also served at `/api/v1/openapi.yaml`). Within v1 it only grows; breaking changes would go in a v2 served alongside it. Kubernetes probes are at `/healthz` (process up) and `/readyz` (database reachable).

## Development

```sh
make test                      # Go tests (SQLite) and web type checks
MATINEE_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/postgres?sslmode=disable make test-postgres
make dev                       # web player with hot reload, against a server from `make run`
```

The Go tests build real media files with FFmpeg and run a full scan, browse, play and transcode cycle, so FFmpeg needs to be installed.

## Releasing

Push a version tag, such as `git tag v0.2.0 && git push origin v0.2.0`. The release workflow tests, then publishes `ghcr.io/yeixio/matinee:0.2.0` (and `0.2`, `latest`) for amd64, arm64 and armv7, pushes the Helm chart to `oci://ghcr.io/yeixio/charts/matinee` with that version, and attaches single-binary archives for Linux, macOS and Windows plus `.deb` and `.rpm` packages to a GitHub release. It then installs the packages from freshly built apt and dnf repositories in Debian, Ubuntu and Fedora containers, publishes those repositories on the `packages` branch, and opens a pull request updating `Formula/matinee.rb` for Homebrew. A tag with a suffix (`v0.2.0-beta.1`) is a pre-release: it leaves `latest`, Homebrew, apt and dnf alone. Packaging details and the signing key setup are in [docs/packaging.md](docs/packaging.md).

## Contributing

Matinee is young, and it's a good time to get involved: a small fix lands quickly, and the big pieces (hardware transcoding, plugins, downloads, music) are still open to shape.

- **Start small.** Issues labeled [good first issue](https://github.com/yeixio/matinee/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) have acceptance criteria and say which file to open first. [help wanted](https://github.com/yeixio/matinee/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22) has bigger ones, sorted by `area/*` and `difficulty/*`.
- **No code needed.** Try Matinee on your NAS, GPU, TV browser or oddly named files and send a [compatibility report](https://github.com/yeixio/matinee/issues/new?template=compatibility_report.yml).
- **Join the core team.** We're looking for people to own areas like streaming, the web player, music, packaging and docs. [Say hello in #51](https://github.com/yeixio/matinee/issues/51).

[CONTRIBUTING.md](CONTRIBUTING.md) gets you from clone to a running server with a demo library. Contributions are covered by a short [CLA](CLA.md), and everyone follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

The Matinee server and web player are licensed under the [GNU AGPL-3.0-or-later](LICENSE).
