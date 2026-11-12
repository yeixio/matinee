---
title: Games
description: Play games on your Matinee server and stream them to a player with a controller. A preview for admins, with a lag test.
since: v0.2.4
sidebar:
  order: 11
---

Matinee can play games for you on the server and stream them to your screen, the same way it streams a film. You connect a controller, pick a game and play: the emulator, its settings and the video all stay on the server, so there's nothing to install or set up on the device you play on.

This is a **preview**. For now only admins can play, in the web player, and you pick the game from a folder. A Games library with box art for everyone, and playing in the phone and TV apps, come next ([#395](https://github.com/yeixio/matinee/issues/395), [#394](https://github.com/yeixio/matinee/issues/394)).

## What it plays

| System | Files |
| --- | --- |
| Game Boy and Game Boy Color | `.gb`, `.gbc` |
| Game Boy Advance | `.gba` |
| Nintendo Entertainment System | `.nes`, `.fds` |

A zip holding one of these works too. Matinee plays only the games you already have: it never looks for, links to or downloads games.

Games with a battery save, such as RPGs, keep their saves on the server, one for each person, and pick up where you saved next time.

## Playing a game

1. Open **Settings → Streaming → Games** and press **Try playing a game**.
2. Connect a controller to the computer and press any button on it, so the browser sees it. Xbox, PlayStation and most other controllers work. Without one, the keyboard works: arrow keys, **Z** and **X** for B and A, **A** and **S** for Y and X, **Enter** for Start.
3. Find the game in the folder list and click it. It opens full screen.
4. Close it with the **×** at the top. A battery save is kept.

The controller buttons go by where they sit, not by their letters: the bottom face button is B and the right one is A, as on the consoles.

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

Matinee plays games with free, open-source emulators, each in its own process so a game that crashes can't take the server with it: [Gambatte](https://github.com/libretro/gambatte-libretro) for the Game Boy, [mGBA](https://mgba.io) for the Game Boy Advance, and [FCEUmm](https://github.com/libretro/libretro-fceumm) for the NES. Their licences are in `/usr/lib/matinee/games/licenses` in the image.
