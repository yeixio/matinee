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

Transcodes and the image cache use a local `emptyDir`, so they're fast and never land on network storage.

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
