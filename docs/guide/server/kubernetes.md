---
title: Kubernetes
description: Run Matinee on Kubernetes with the Helm chart, with its bundled PostgreSQL, your media mounts, a GPU, and automatic updates.
sidebar:
  order: 11
---

Matinee has a Helm chart. It runs the server with a bundled PostgreSQL whose credentials are generated into a Secret on the first install, so there's nothing to set up, and it works the same under Argo CD or Flux.

```sh
helm install matinee oci://ghcr.io/yeixio/charts/matinee --version 0.1.0 -f my-values.yaml
```

## A starting point

```yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: matinee.example.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: matinee-tls
      hosts: [matinee.example.com]

tmdb:
  existingSecret: matinee-tmdb   # see "Posters and details"

media:
  - name: movies
    mountPath: /media/movies
    volume:
      nfs: { server: nas.local, path: /volume1/movies, readOnly: true }
  - name: tv
    mountPath: /media/tv
    volume:
      persistentVolumeClaim: { claimName: tv-share, readOnly: true }
```

Each entry in `media` mounts any Kubernetes volume (NFS, SMB through a CSI driver, a PVC, a host path) read-only under `mediaRoot` (`/media`), with an optional `subPath`. When you [add a library](/docs/getting-started/first-run/), the folder picker opens there.

For the TMDB key, create the Secret once:

```sh
kubectl create secret generic matinee-tmdb --from-literal=tmdb-api-key=your-key
```

## The values that matter

| Value | Default | What it's for |
| --- | --- | --- |
| `ingress.*` | off | How you reach Matinee. Set your host and TLS. |
| `tmdb.existingSecret` | empty | The Secret with your TMDB key. |
| `media` | none | Your media folders. |
| `downloads.persistence` | on, 50Gi | Where [downloads for the apps](/docs/apps/downloads/) are prepared. Set `storageClass` or `existingClaim`. |
| `plugins.persistence` | on, 5Gi | Where installed [plugins](/docs/server/plugins/) and what they keep live. Set `storageClass` or `existingClaim`. |
| `postgresql.enabled` | `true` | The bundled database. To use your own, turn it off and set `database.url` or `database.existingSecret`. |
| `discovery.*` | on | Lets the apps find the server at home; see below. |
| `remoteAccess.*` | off | Watching away from home when the ingress only works at home; see below. |
| `gpu.type` | `none` | `vaapi` (AMD, Intel) or `nvidia` for [hardware transcoding](/docs/server/transcoding/). |
| `nodeSelector`, `tolerations`, `affinity` | none | Keep Matinee on the node with the GPU, for example. |
| `env` | none | Any other [setting](/docs/server/configuration/), such as `MATINEE_RATING_COUNTRY`. |
| `resources` | none | Requests and limits. Transcoding without a GPU wants several CPU cores. |

<!-- since: v0.1.11 -->
`thumbnails.persistence` (on, 20Gi) keeps the [seek bar previews](/docs/watching/seek-previews/), about 4 MB per movie. Set `storageClass` or `existingClaim`, or a bigger `size` for a big library.
<!-- /since -->

<!-- since: v0.2.4 -->
`logs.persistence` (on, 1Gi) keeps the logs of the last 10 runs, so a crash's errors can still be read in [Server health](/docs/server/server-health/#earlier-runs-and-crashes) from the pod that replaced it. Set `storageClass` or `existingClaim`.
<!-- /since -->

Transcodes and the image cache use a local `emptyDir`, so they're fast and never land on network storage.
<!-- since: v0.2.4 -->
Its size is `cache.sizeLimit` (50Gi). Kubernetes evicts the pod if the `emptyDir` grows past it, so the chart tells Matinee the limit, and transcodes keep within it: a film bigger than the room left keeps only the minutes around where it plays ([more](/docs/server/transcoding/#when-the-transcode-folder-runs-short-of-room)). 50Gi plays any film; more room lets people seek back further without waiting.
<!-- /since -->

<!-- since: v0.2.4 -->
### Volume sizes

Many storage classes can't grow a volume once it's made, `local-path` among them, and Kubernetes then refuses the whole upgrade if a bigger `size` is asked of one ("only dynamically provisioned pvc can be resized"). With plain `helm upgrade`, the chart notices and keeps the size the volume already has. Argo CD and Flux render the chart without looking at the cluster, so tell them to leave volume sizes as they are:

```yaml
spec:
  ignoreDifferences:
    - kind: PersistentVolumeClaim
      jsonPointers:
        - /spec/resources/requests/storage
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
```

To really make a volume bigger on such a class, make a new claim of the size you want, point `existingClaim` at it, and delete the old one.
<!-- /since -->

<!-- since: v0.2.4 -->
### Prometheus and alerts

With the Prometheus Operator (kube-prometheus-stack, say), the chart can have Prometheus scrape Matinee's [metrics](/docs/server/alerts/#prometheus-metrics) and alert when playback gets slow or the server leaks. Turn metrics on in **Settings → Server health → Alerts**, put the token in a Secret, and turn on both:

```sh
kubectl -n media create secret generic matinee-metrics --from-literal=token=<the metrics token>
```

```yaml
metrics:
  tokenSecret: {name: matinee-metrics, key: token}
  serviceMonitor:
    enabled: true
    labels: {release: kube-prometheus-stack} # what your Prometheus selects by
  prometheusRule:
    enabled: true
    labels: {release: kube-prometheus-stack}
```

| Alert | When |
| --- | --- |
| `MatineeDown` | Prometheus can't reach Matinee for 5 minutes. |
| `MatineeSlowStarts` | 1 in 10 starts and seeks takes over 8 seconds (`slowStartSeconds`) for 15 minutes. |
| `MatineeBuffering` | Players wait over 5 seconds for more than 1 in 20 pieces for 10 minutes. |
| `MatineeTranscodesTooSlow` | A transcode makes video slower than it plays for 5 minutes. |
| `MatineeFFmpegNotStopping` | FFmpeg keeps running after its transcode ended, for 5 minutes. |
| `MatineeGoroutinesGrowing`, `MatineeOpenFilesGrowing`, `MatineeMemoryGrowing` | Signs of a leak: past `maxGoroutines`, `maxOpenFiles`, or `maxHeapBytes` and still climbing. |

Add your own under `metrics.prometheusRule.extraRules`.

`metrics.grafanaDashboard.enabled` adds a Grafana dashboard, **Matinee / Playback and resources**, for Grafana's dashboard sidecar to pick up. Set `namespace` to where the sidecar looks (often `monitoring`). It shows how long plays and seeks take to start, buffering, FFmpeg processes against transcodes, memory, goroutines, open files and CPU.
<!-- /since -->

<!-- since: v0.2.7 -->
### AppArmor

Some kernel updates break the default AppArmor profile that the container runtime gives every container: programs in a container can no longer send each other signals. The bundled PostgreSQL then never finishes starting, its log says `could not signal for checkpoint: Permission denied`, and Matinee waits for its database. Until the node's kernel is fixed (or you boot the previous one), run Matinee's pods without that profile:

```yaml
appArmorProfile:
  type: Unconfined
```

It applies to the server, discovery and the bundled PostgreSQL. Leave it empty (the default) to keep the node's own profile.
<!-- /since -->

## Replicas

Run one replica for now: a transcode in progress lives in the pod that started it. More replicas work with `service.sessionAffinity: ClientIP`, so each device keeps talking to the same pod.

## The apps at home

The chart runs a small announcer on a node's host network so the apps find the server at home, pointing them at your ingress address. It also opens port 7172 on Matinee's node (`discovery.homePort`) as the [home network's own way in](/docs/server/home-network/#how-matinee-knows-youre-at-home), so devices at home can watch without signing in while everything through the ingress signs in. Set `discovery.homePort=0` to turn that off, `discovery.url` if your ingress is managed outside the chart, and `discovery.enabled=false` to keep the server unlisted.

<!-- since: v0.1.1 -->
## Watching away from home

If your ingress is reachable from the internet, it's the way in from outside and there's nothing to set. If it only works at home, set `remoteAccess.enabled=true` and Matinee sets up [remote access](/docs/server/remote-access/) itself: it asks your router for a port and gets its own certificate. Talking to the router takes the node's own network, so the pod then runs on its node's host network, with ports 7171, the home port and `remoteAccess.port` (7443) opened on that node. Its key, certificate and [Matinee Connect](/docs/server/remote-access/#matinee-connect) subscription go on a small volume (`remoteAccess.persistence`) so the server keeps its identity across restarts. Don't set `MATINEE_PUBLIC_URL` alongside it.

## Following the latest build

To try each change as it lands, point Argo CD at the `edge` branch. It moves to a commit on `main` only once that commit's checks pass and its image is published, and the chart from that commit runs the image built from it:

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

**Settings** then shows the commit you run as `main-<commit>`. For releases instead, install from `oci://ghcr.io/yeixio/charts/matinee` with a version.

<!-- since: v0.2.3 -->
## Updating from Settings

Set `updates.argoCD.application` to the Application's name and Matinee can move it to a newer version itself, from releases or from main, automatically or when you press **Update**. See [Updates](/docs/server/updates/#kubernetes-with-argo-cd) for the setup, including what an app of apps needs to leave alone.
<!-- /since -->
