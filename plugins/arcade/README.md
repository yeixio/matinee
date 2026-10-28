# Arcade plugin

Plays arcade games in Matinee's web player with [MAME](https://www.mamedev.org)
built to WebAssembly. The [user guide](https://matinee.yeix.io/docs/server/arcade/)
covers using it; this is about building it. It's also the reference for
[writing plugins](../../docs/plugins.md).

- `main.go`: the server part. It files each ROM set in an Arcade library
  using MAME's list of games (`gamedb`), picks up artwork from `snap`,
  `titles` and `flyers` folders, and looks up the rest online.
- `player/`: the player. It hands the game's ROM sets to MAME and keeps each
  person's saved state in the plugin's storage.
- `drivers.txt`: the MAME drivers the player can run. Each is built on its
  own (`build-mame.sh`), so a game loads only the emulation it needs. Add a
  line to make more games playable.
- `mame-version`, `emsdk-version`: the MAME release and Emscripten version
  the builds use.

`.github/workflows/arcade.yml` builds everything, plays gridlee (one of the
ROMs [mamedev.org](https://www.mamedev.org/roms/) gives away) in Chromium,
and on main publishes a new `version` in `plugin.json` as a release.

To build by hand, with Go and Emscripten:

```sh
cd plugins/arcade
go test ./...
go run ./cmd/gamedb mame0289lx.zip games.json.gz       # MAME's XML list
./build-mame.sh ~/src/mame pacman/pacman.cpp builds     # needs emmake
./package.sh games.json.gz builds ~/src/mame arcade.matinee-plugin
```

Then add `arcade.matinee-plugin` in Settings → Plugins → From a file.

The plugin's own code is under Matinee's license (AGPL-3.0-or-later). MAME
is GPL-2.0-or-later; the package carries both licenses.
