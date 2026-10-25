# Contributing

Thanks for wanting to help. Matinee is young, so a small fix lands quickly and you'll see it in the next release. We're also [looking for core team members](https://github.com/yeixio/matinee/issues/51).

## Find something to work on

- [**good first issue**](https://github.com/yeixio/matinee/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22): small and well-scoped, with acceptance criteria and a pointer to the file to start in.
- [**help wanted**](https://github.com/yeixio/matinee/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22): open for anyone. Check the `difficulty/easy`, `difficulty/medium` and `difficulty/hard` labels.
- `area/*` labels say which part of the code an issue touches: `area/scanning`, `area/metadata`, `area/streaming`, `area/server`, `area/web`, `area/music`, `area/plugins`, `area/deploy`, `area/ci`.
- `needs design` means the issue wants a short written plan, agreed in the comments, before code. Writing that plan is a great contribution in itself.
- `hardware` means it needs someone with a particular GPU, NAS or TV to build or test it.

Comment on an issue before you start, so two people don't do the same work. If nobody replies within a few days, go ahead. For a `difficulty/hard` issue, sketch your approach in the comments first.

Not writing code? Trying Matinee on your NAS, with your file names, or on your TV's browser, and reporting what happened, is just as useful.

## Get it running

You need Go 1.26+, Node 22 with pnpm, and FFmpeg.

```sh
git clone https://github.com/yeixio/matinee.git
cd matinee
make demo   # a small fake library in ./demo-media, made with FFmpeg
make run    # builds the web player and server, serves http://localhost:7171
```

Open http://localhost:7171, create the admin account, and add the folders in `./demo-media` as libraries.

For web work, keep `make run` going and run `make dev` in a second terminal: the web player hot-reloads against your server.

A TMDB key is optional. Everything except online posters works without one ([how to get one](README.md#posters-and-descriptions-tmdb-key)).

## Where things are

```
cmd/matinee          entry point and flags
internal/api         HTTP API, auth, serves the web player
internal/scan        library scanning and file name parsing (parse.go)
internal/metadata    TMDB and MusicBrainz
internal/artwork     poster and backdrop serving and caching
internal/stream      direct play and HLS transcoding
internal/subtitle    subtitle discovery and conversion
internal/store       SQLite / PostgreSQL storage and migrations
web/src              React web player (pages/, components/, player/)
deploy/helm/matinee  Helm chart
```

## Test and lint

```sh
make test    # Go tests (SQLite), web type checks and tests
make lint    # go vet and gofmt
```

The Go tests make real media files with FFmpeg and run a full scan, browse, play and transcode cycle. CI also runs them on PostgreSQL. To do that locally:

```sh
MATINEE_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/postgres?sslmode=disable make test-postgres
```

Run `gofmt -w` on Go files you touch.

## Style

- Follow the code around you: small packages, plain functions, short comments that say why.
- The same SQL runs on SQLite and PostgreSQL. A schema change is a new entry in the migrations list in `internal/store/store.go`, never an edit to an old one.
- The HTTP API is what the apps talk to. Add fields and routes; don't rename or remove them.
- In the web player, use the colors and spacing already defined in `web/src/styles.css`, handle loading, error and empty states, and check your change at 390 px wide.
- UI text is short and plain: "Add library", not "Click here to add a new library to your server".

## Pull requests

- Keep each pull request to one change, and link the issue it fixes (`Fixes #123`).
- Say how you tested it. For UI changes, add a screenshot, and a phone-width one if the layout changed.
- CI must be green: Go on SQLite and PostgreSQL, the web build, the Helm lint and the Docker build.
- A maintainer reviews within a few days. Requested changes are normal, not a rejection.

Using AI tools to help is fine. You're responsible for what you submit, so read and test it before you open the pull request.

## Contributor License Agreement

Contributors retain copyright ownership of their contributions.

By submitting a contribution to Matinee, you agree to the [Matinee Contributor License Agreement](CLA.md), which grants YEIXIO LLC the rights necessary to use, modify, distribute, sublicense, and relicense contributed code, including as part of commercial offerings.

This allows Matinee to remain open source under the AGPL while preserving future commercial licensing options. The pull request template has a checkbox for it.

## Issues

Use the forms when you open an issue: bug, feature, compatibility report, or docs. Remove passwords, API keys and private URLs from logs.

Security problems go through [SECURITY.md](SECURITY.md), not a public issue. Code of Conduct reports go to conduct@yeix.io (see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)).

## Labels

Labels are defined in `.github/labels.yml`. Maintainers sync them with `scripts/sync-github-labels.sh`, which needs the GitHub CLI and permission to manage labels.

## Growing into a role

Regular contributors can become triagers, reviewers and maintainers. [GOVERNANCE.md](GOVERNANCE.md) describes each role and [MAINTAINERS.md](MAINTAINERS.md) lists who holds them. Every release thanks the people whose work is in it.
