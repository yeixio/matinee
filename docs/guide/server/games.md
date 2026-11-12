---
title: Games
description: Play games on your Matinee server and stream them to a player with a controller, with the built-in emulators or your own. A preview for admins, with a lag test.
since: v0.2.4
sidebar:
  order: 11
---

Matinee can play games for you on the server and stream them to your screen, the same way it streams a film. You connect a controller, pick a game and play: the emulator, its settings and the video all stay on the server, so there's nothing to install or set up on the device you play on.

This is a **preview**. For now only admins can play, in the web player, and you pick the game from a folder. A Games library with box art for everyone, and playing in the phone and TV apps, come next ([#395](https://github.com/yeixio/matinee/issues/395), [#394](https://github.com/yeixio/matinee/issues/394)).

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

A zip holding one of these works too, including a zip of a PlayStation disc's `.cue` and `.bin` files. Matinee plays only the games you already have: it never looks for, links to or downloads games. You can add emulators for other systems yourself (see [Adding your own emulator](#adding-your-own-emulator)).

None of these needs a graphics card in the server: the N64 picture is drawn in software, which wants a recent CPU with a few cores to spare. The Super Nintendo and N64 emulators aren't in the image for 32-bit ARM boards, which are too slow for them.

The PlayStation emulator doesn't need the console's BIOS, but plays more games, more faithfully, with one: put your own BIOS file (such as `scph5501.bin`) in `games/system` in Matinee's data folder.

Games with a battery save, such as RPGs, keep their saves on the server, one for each person, and pick up where you saved next time.

## Playing a game

1. Open **Settings → Streaming → Games** and press **Try playing a game**.
2. Connect a controller to the computer and press any button on it, so the browser sees it. Xbox, PlayStation and most other controllers work. Without one, the keyboard works: arrow keys, **Z** and **X** for B and A, **A** and **S** for Y and X, **Enter** for Start.
3. Find the game in the folder list and click it. It opens full screen.
4. Close it with the **×** at the top. A battery save is kept.

The controller buttons go by where they sit, not by their letters: the bottom face button is B and the right one is A, as on the consoles. For the N64 the left stick is the N64's stick and the right stick its C buttons; on the keyboard the arrow keys move the stick.

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

The Docker image and the Helm chart include the game runner and the emulators. Other installs don't have them yet, and **Settings → Streaming → Games** says so.

## The emulators

Matinee plays games with free, open-source emulators, each in its own process so a game that crashes can't take the server with it: [Gambatte](https://github.com/libretro/gambatte-libretro) for the Game Boy, [mGBA](https://mgba.io) for the Game Boy Advance, [FCEUmm](https://github.com/libretro/libretro-fceumm) for the NES, [bsnes](https://github.com/libretro/bsnes-libretro) for the Super Nintendo, [clownmdemu](https://github.com/Clownacy/clownmdemu-libretro) for the Genesis, [ParaLLEl N64](https://github.com/libretro/parallel-n64) for the N64 and [PCSX ReARMed](https://github.com/libretro/pcsx_rearmed) for the PlayStation. Their licences are in `/usr/lib/matinee/games/licenses` in the image.

## Adding your own emulator

You can add any emulator that's a **libretro core**, for a system Matinee doesn't play yet, or to play a system with a different emulator than the built-in one.

1. Get the core built for your server: a file ending in `_libretro.so` for Linux on the server's kind of CPU. **Settings → Streaming → Games** says which one you need, such as **Linux x86_64** for most PCs or **Linux aarch64** for a Raspberry Pi 4 or 5. The libretro project's [nightly builds](https://buildbot.libretro.com/nightly/linux/) have most cores, zipped.
2. Unzip it, and in **Settings → Streaming → Games → Emulators** press **Add an emulator** and pick the `.so` file.
3. Matinee loads it to ask which files it plays. If that works it's added, listed with the files it plays, and those games play with it from then on, ahead of a built-in emulator for the same files. If it doesn't, Matinee says why (built for another kind of computer, say) and doesn't keep it.

An emulator is a program that runs on your server, so add only ones you trust, such as libretro's own builds. Each runs in its own process like the built-in ones.

You can also copy cores into `games/cores` in Matinee's data folder by hand; Matinee picks them up the next time it lists games. To remove one, press the bin next to it, or delete the file.

Some emulators need a BIOS file from the console. Put it in `games/system` in Matinee's data folder, named as the emulator's own documentation says. Emulators that draw with a graphics card (OpenGL or Vulkan) can't run on the server yet. An arcade emulator that plays zips is given the zip as it is, so the zip has to be the set of game files that emulator's version expects.
