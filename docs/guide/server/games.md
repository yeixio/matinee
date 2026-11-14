---
title: Games
description: Play games on your Matinee server and stream them to a player with a controller. A Games library with box art for everyone, built-in emulators or your own, and a lag test.
since: v0.2.4
sidebar:
  order: 11
---

Matinee can play games for you on the server and stream them to your screen, the same way it streams a film. You connect a controller, pick a game and play: the emulator, its settings and the video all stay on the server, so there's nothing to install or set up on the device you play on.

This is a **preview**. You play in the web player and in the phone and TV apps ([below](#in-the-apps)).

## What it plays

| System | Files | Emulator |
| --- | --- | --- |
| Game Boy and Game Boy Color | `.gb`, `.gbc` | Gambatte |
| Game Boy Advance | `.gba` | mGBA |
| Nintendo Entertainment System | `.nes`, `.fds` | FCEUmm |
| Super Nintendo | `.sfc`, `.smc` | bsnes |
| Sega Genesis / Mega Drive | `.md`, `.gen`, `.bin` | clownmdemu |
| Nintendo 64 | `.n64`, `.v64`, `.z64` | ParaLLEl N64 |
| PlayStation | `.cue` (with its `.bin` tracks beside it), `.chd`, `.pbp`, `.m3u`, `.iso` | PCSX ReARMed |

<!-- since: v0.2.7 -->
From 0.2.7 it also plays:

| System | Files | Emulator |
| --- | --- | --- |
| Arcade games | MAME 0.289 sets, one `.zip` or `.7z` per game (see [Arcade games](/docs/server/arcade/)) | MAME |
| Atari 2600, 5200 and 7800 | `.a26`, `.a52`, `.a78` | Stella 2014, a5200, ProSystem |
| Atari Lynx and Jaguar | `.lnx`, `.j64`, `.jag` | Beetle Lynx, Virtual Jaguar |
| Sega Master System and Game Gear | `.sms`, `.gg` | Gearsystem |
| ColecoVision | `.col` | Gearcoleco |
| Intellivision | `.int` | FreeIntv |
| PC Engine / TurboGrafx-16 | `.pce` | Beetle PCE Fast |
| Neo Geo Pocket and Pocket Color | `.ngp`, `.ngc` | Beetle NeoPop |
| WonderSwan and WonderSwan Color | `.ws`, `.wsc` | Beetle Cygne |
| Virtual Boy | `.vb` | Beetle VB |
| Commodore 64 | `.d64`, `.t64`, `.prg`, `.crt`, `.g64` | VICE |
| Amiga | `.adf`, `.ipf` | PUAE |
| ZX Spectrum | `.tzx`, `.z80`, `.sna` | Fuse |
| DOS PCs | a zip of the game's folder | DOSBox Pure |

ColecoVision and Intellivision games need the console's own BIOS: put yours in `games/system` in Matinee's data folder (`colecovision.rom` for the ColecoVision, `exec.bin` and `grom.bin` for the Intellivision). The Amiga plays best with your own Kickstart ROM there too (`kick34005.A500` and the like). A DOS game is played from the zip in a folder named for it, such as `pc` or `dos`. The arcade, Atari Jaguar, Commodore 64, Amiga and DOS emulators aren't in the image for 32-bit ARM boards.
<!-- /since -->

A zip or 7z holding one of these works too, including a zip of a PlayStation disc's `.cue` and `.bin` files. When an archive holds several dumps of a game, as GoodTools sets do, Matinee plays the verified good one (`[!]`), preferring the US or European release, over bad dumps, overdumps, hacks, translations and betas. Matinee plays only the games you already have: it never looks for, links to or downloads games. You can add emulators for other systems yourself (see [Adding your own emulator](#adding-your-own-emulator)).

None of these needs a graphics card in the server: the N64 picture is drawn in software, which wants a recent CPU with a few cores to spare. The Super Nintendo and N64 emulators aren't in the image for 32-bit ARM boards, which are too slow for them.

The PlayStation emulator doesn't need the console's BIOS, but plays more games, more faithfully, with one: put your own BIOS file (such as `scph5501.bin`) in `games/system` in Matinee's data folder.

Games with a battery save, such as RPGs, keep their saves on the server, one for each person, and pick up where you saved next time.

## A Games library

Add your games as a library and everyone you share it with can browse them with box art and play them, like films.

1. In **Settings → Libraries** press **Add library**, pick **Games** and add the folder your games are in.
2. Matinee finds each game and fills in its name, year, system, region, publisher, number of players and genre, and its box art.

Games can be named and sorted any way you like. Matinee tells the system by the folder a game is in when the folder is named for it (`Game Boy`, `gba`, `snes`, `Mega Drive`, `PlayStation` and so on), otherwise by what's in a zip or the file's ending. It then recognizes the game by its contents, so even `tetris copy.gb` is found as Tetris (World) (Rev 1), and falls back to the file's name for games it doesn't know, such as homebrew.

For PlayStation games of more than one disc, keep an `.m3u` list beside the discs: the game shows once, and you change discs in the game.

<!-- since: v0.2.7 -->
Arcade sets go in the same library, in a folder such as `mame` or `arcade`: see [Arcade games](/docs/server/arcade/).

**Every game shows**, even one for a system Matinee has no emulator for yet, such as the Nintendo Switch, GameCube or PlayStation 2. Its page says so instead of offering **Play**, with a link for admins to [add an emulator](#adding-your-own-emulator) for it.

**A game shows once.** The same game for another region or a later revision, such as `Super Game (Europe)` next to `Super Game (USA)`, is a version of it, like a film's versions: the page shows the release most likely wanted (the USA's or the world's, then Europe's), and an arcade game's clones sit under it the same way.

If a file doesn't show up, open the Games library: admins see a note at the top saying how many files were left out and why, grouped by folder, such as zips that hold neither a console's game nor one of MAME's sets.
<!-- /since -->

<!-- since: v0.2.7 -->
### Details

Each game gets a page like a film's: its box art, a screenshot behind it, the year, who made it, how many play, and a summary from the start of its [Wikipedia](https://en.wikipedia.org) article, with its genres from Wikidata. Set `MATINEE_WIKIPEDIA=false` to skip the summaries.
<!-- /since -->

To recognize games Matinee downloads the [libretro database](https://github.com/libretro/libretro-database)'s lists for each system once and keeps them for a month, in `games/lists` in Matinee's data folder; box art comes from [libretro's thumbnails](https://thumbnails.libretro.com). Without internet, games are named by their files and have no box art. Nothing about your games is sent anywhere.

<!-- since: v0.2.6 -->
### Box art

Matinee tries hard to find a picture for every game, in this order:

1. **Your own picture.** An image named like the game beside it, such as `Tetris (World).png` next to `Tetris (World).gb`, or in a folder other game apps and scrapers keep pictures in: `boxart`, `covers`, `images`, `media/box2dfront`, `downloaded_media/covers` and the like. Names ending in `-image`, `-boxart` or `-cover` count too.
2. **libretro's box art** for the game, or the same game's from another region (the USA's, then the world's, Europe's and Japan's) when yours has none. Names that differ only by a "The", an "&" or punctuation still match.
3. **libretro's title screen**, or failing that a screenshot, when there's no box art at all.

To pick from what libretro has, Matinee keeps its list of pictures for each system for a month, in `games/lists/art`.

### Picking a system

In a Games library with games for more than one system, the systems sit above the games: pick one to see only its games, or **All**. **Filters** has them too, to pick several.
<!-- /since -->

## Playing a game

1. Connect a controller to the computer and press any button on it, so the browser sees it. Xbox, PlayStation and most other controllers work. Without one, the keyboard works: arrow keys, **Z** and **X** for B and A, **A** and **S** for Y and X, **Enter** for Start.
2. Open a game in your Games library and press **Play**. It opens full screen.
3. Close it with the **×** at the top. A battery save is kept.

Admins can also play any game file on the server without adding it to a library: in **Settings → Streaming → Games** press **Try playing a game** and find it in the folder list.

The controller buttons go by where they sit, not by their letters: the bottom face button is B and the right one is A, as on the consoles. For the N64 the left stick is the N64's stick and the right stick its C buttons; on the keyboard the arrow keys move the stick.

<!-- since: v0.2.4 -->
### In the apps

The iPhone, iPad and Android apps, Android TV, Fire TV, and Samsung and LG TVs play games from your Games library too. Open a game and press **Play**.

- **On a phone or tablet** the game turns sideways and shows buttons on the screen: a d-pad, A, B, X, Y, L, R, Select and Start. Use both thumbs at once. On the N64 the d-pad moves the stick.
- **With a controller**, pair it with the phone, tablet or TV in its Bluetooth settings first. The buttons on the screen make way when it connects; tap the screen to bring them back. Up to four controllers play at once, each as its own player.
- **To stop**, press the **×** at the top, the TV remote's **Back**, or the controller's own home button.
<!-- /since -->

<!-- since: v0.2.5 -->
#### On Apple TV

From app version 1.0.40, Apple TV plays games too, with a server from 0.2.5 on. Pair the controller under **Settings → Remotes and Devices → Bluetooth** on the Apple TV. Apple TV gets the game straight over the connection it already has to the server, so it doesn't need the [games port](#the-games-port). Press the Siri Remote's **Back** button (**Menu** on older remotes), or the controller's own home button, to stop.
<!-- /since -->

## How fast it is

Every press goes to the server and the picture comes back, so there's a little more delay than playing on the device itself. On a home network it's usually a few hundredths of a second, which suits most games. A wired connection helps the most.

The **Lag test** on the games page measures it for you. Move the square with the d-pad, and hold **A** or **B**: the screen turns white, and **Measured, press to picture** shows how long that took to reach your player. Add your screen's own delay, often 10 to 30 ms with a TV's game mode on. **Show lag** also breaks it down while you play a game.

## The games port

Games stream over **UDP port 7173**, separately from the web player's port. On the home network nothing needs setting up. In a container, publish the port as UDP, such as `-p 7173:7173/udp` with Docker. The Helm chart opens it on the node by itself (`games.port`, 0 turns games off).

| Environment variable | Default | What it's for |
| --- | --- | --- |
| `MATINEE_GAMES_PORT` | `7173` | The UDP port games stream through. `0` picks ports at random. |
| `MATINEE_GAMES_PUBLIC_IP` | | The addresses players reach that port at, comma separated, when the server can't see its own, such as in a container that isn't on the host network. The Helm chart sets it to the node's address. |
| `MATINEE_GAMES_DIR` | `/usr/lib/matinee/games` | Where the game runner and emulators are. `off` turns games off. |

The Docker image, the Helm chart and, from 0.2.7, the apt and dnf packages, Matinee Server.app on Macs with Apple silicon and the Windows installer (on Intel, AMD and Arm PCs alike) include the game runner and the emulators, so there's nothing to install. Homebrew doesn't have emulators yet: its Games library shows your games, and **Settings → Streaming → Games** says this server can't play them.

## The emulators

Matinee plays games with free, open-source emulators, each in its own process so a game that crashes can't take the server with it: [Gambatte](https://github.com/libretro/gambatte-libretro) for the Game Boy, [mGBA](https://mgba.io) for the Game Boy Advance, [FCEUmm](https://github.com/libretro/libretro-fceumm) for the NES, [bsnes](https://github.com/libretro/bsnes-libretro) for the Super Nintendo, [clownmdemu](https://github.com/Clownacy/clownmdemu-libretro) for the Genesis, [ParaLLEl N64](https://github.com/libretro/parallel-n64) for the N64 and [PCSX ReARMed](https://github.com/libretro/pcsx_rearmed) for the PlayStation. Their licences are in `/usr/lib/matinee/games/licenses` in the image.

<!-- since: v0.2.7 -->
From 0.2.7 there are also [MAME](https://www.mamedev.org) for arcade games, and [Stella](https://stella-emu.github.io), a5200, ProSystem, Beetle (from [Mednafen](https://mednafen.github.io)), Virtual Jaguar, [Gearsystem](https://github.com/drhelius/Gearsystem), [Gearcoleco](https://github.com/drhelius/Gearcoleco), FreeIntv, [VICE](https://vice-emu.sourceforge.io), PUAE, [Fuse](https://fuse-emulator.sourceforge.net) and [DOSBox Pure](https://github.com/schellingb/dosbox-pure) for the systems in the table above.
<!-- /since -->

## Adding your own emulator

You can add any emulator that's a **libretro core**, for a system Matinee doesn't play yet, or to play a system with a different emulator than the built-in one.

1. Get the core built for your server: a file ending in `_libretro.so` for Linux, `_libretro.dll` for Windows or `_libretro.dylib` for a Mac, for the server's kind of CPU. **Settings → Streaming → Games** says which one you need, such as **Linux x86_64** for most Linux PCs, **Linux aarch64** for a Raspberry Pi 4 or 5, **Windows x86_64** or **macOS arm64**. The libretro project's [nightly builds](https://buildbot.libretro.com/nightly/) have most cores, zipped.
2. Unzip it, and in **Settings → Streaming → Games → Emulators** press **Add an emulator** and pick the file.
3. Matinee loads it to ask which files it plays. If that works it's added, listed with the files it plays, and those games play with it from then on, ahead of a built-in emulator for the same files. If it doesn't, Matinee says why (built for another kind of computer, say) and doesn't keep it.

<!-- since: v0.2.7 -->
You can also add one straight from a game's page: a game Matinee can't play yet says so, and admins get an **Add an emulator** button right there. Once the emulator is added, the game's **Play** button appears.
<!-- /since -->

An emulator is a program that runs on your server, so add only ones you trust, such as libretro's own builds. Each runs in its own process like the built-in ones.

You can also copy cores into `games/cores` in Matinee's data folder by hand; Matinee picks them up the next time it lists games. To remove one, press the bin next to it, or delete the file.

Some emulators need a BIOS file from the console. Put it in `games/system` in Matinee's data folder, named as the emulator's own documentation says. Emulators that draw with a graphics card (OpenGL or Vulkan) can't run on the server yet. An arcade emulator that plays zips is given the zip as it is, so the zip has to be the set of game files that emulator's version expects.

<!-- since: v0.2.6 -->
## PC games from Steam (preview)

Matinee can also stream PC games from Steam. The games don't run on the Matinee server: they run on a **game host**, a Linux computer with a graphics card and Steam on it, such as a gaming PC. The game host plays the game, records its picture and sound on its graphics card, and sends them to Matinee, which streams them to your player the same way it streams console games. Your controller works as a controller plugged into the game host.

Only the games you own and have installed in Steam on the game host are listed. Matinee never downloads games.

This is an early preview, played from **Games** in the web app by an admin:

1. On the game host, sign in to Steam and install the games you want to play.
2. Run `matinee-gamehost` on the game host, as the user Steam runs as, with a secret token of your own:

   ```sh
   MATINEE_GAMEHOST_TOKEN=pick-a-long-secret matinee-gamehost
   ```

   It listens on port `7174`. It needs FFmpeg, a desktop session for the game to draw on (it uses `DISPLAY`, `:0` if unset), PulseAudio or PipeWire for the sound, and write access to `/dev/uinput` for the controllers. For now, build it from Matinee's source with `go build ./cmd/matinee-gamehost`.
3. Tell the Matinee server where it is:

   | Setting | What it's for |
   | --- | --- |
   | `MATINEE_GAME_HOST` | The game host's address, such as `http://gaming-pc:7174`. |
   | `MATINEE_GAME_HOST_TOKEN` | The same token as the game host's. |

4. Open **Games** in the web app. Under **PC games**, **Steam** opens Steam's Big Picture full screen, where you pick, play and quit games with your controller, as on a TV console. Below it, each installed game starts on its own.

The game host plays one game at a time. Closing the player quits the game, or leaves Big Picture.

<!-- since: v0.2.7 -->
**PC games** also sits at the top of each Games library, above the console games. If the game host is set up but isn't answering, PC games says why instead of disappearing, such as when the game host computer is off or `matinee-gamehost` isn't running.
<!-- /since -->

### Signing in to Steam from Matinee

If the game host's computer has no screen of its own, such as Steam in a container, give the game host the address of a web page that shows its screen (`MATINEE_GAMEHOST_DESKTOP`, such as the noVNC page a Steam container serves). **Games → PC games → Steam desktop** then opens that screen in your browser, through Matinee, so you can sign in to Steam and install games without opening another port. Only admins can open it: it controls the whole computer.

| Game host setting | Default | What it's for |
| --- | --- | --- |
| `MATINEE_GAMEHOST_LISTEN` | `:7174` | The address it listens on. |
| `MATINEE_GAMEHOST_TOKEN` | | The token Matinee must send. Required. |
| `MATINEE_GAMEHOST_HOME` | your home folder | Where to look for Steam. |
| `MATINEE_GAMEHOST_VAAPI` | `/dev/dri/renderD128` | The graphics card to encode on. Without one, it encodes on the CPU, which is slower. |
| `MATINEE_GAMEHOST_AUDIO` | the default output | The PulseAudio source to record the sound from. |
| `MATINEE_GAMEHOST_BITRATE` | `20` | The video bitrate in megabits per second. |
| `MATINEE_GAMEHOST_DESKTOP` | | The address of a web page showing the game host's screen, such as `http://127.0.0.1:8083`, opened by **Steam desktop**. |
<!-- /since -->
