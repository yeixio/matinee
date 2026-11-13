---
title: Arcade games
description: Arcade games in your Games library, played on the server with MAME and streamed to the web player, phones and TVs.
since: v0.2.7
sidebar:
  order: 12
---

Arcade games are part of the [Games library](/docs/server/games/). Matinee plays them on the server with [MAME](https://www.mamedev.org), the same way it plays console games, so they work wherever games do: the web player, phones and tablets, and TVs. There's nothing to install.

## Setting up the folder

Put your MAME ROM sets in a folder of the Games library named `mame` or `arcade` (or point a Games library at your MAME folder):

```text
Games/
  mame/
    pacman.zip
    puckman.zip
    galaga.zip
    neogeo.zip
    kinst/          a game's hard disk images (CHDs), in a folder named for it
      kinst.chd
  snes/
    ...
```

- **One zip or 7z file per game**, named the way MAME names it (`pacman.zip`).
- **ROM sets for MAME 0.289**, the version Matinee plays them with. Sets for other versions mostly work too; a game whose ROMs changed since won't start.
- **BIOS and device sets**, such as `neogeo.zip` or `qsound.zip`, stay in the same folder. They don't show as games, but the games that need them use them.
- **Clones** show under their parent game, as its versions, so each game shows once. With merged sets, clones are inside their parent's zip.
- Folders such as `artwork`, `samples`, `cheat` and `snap` are skipped.

A set that ended up in a console's folder is still recognized as an arcade game, as long as it's named for one of MAME's games and holds no console's game.

## Names, pictures and details

Each game is named from MAME's own list of machines, with its year, maker, number of players and whether its screen is upright. Matinee downloads that list from MAME's releases the first time and keeps it in `games/mame` in its data folder. Box art and screenshots come from [libretro's thumbnails](https://thumbnails.libretro.com), unless you have your own pictures beside the games, and the summary from Wikipedia, as for [every game](/docs/server/games/#details).

## Playing

Open a game and press **Play**. On a controller, **Select** (or **Back**) inserts a coin and **Start** starts; the stick or d-pad moves and the face buttons are the game's buttons. On the keyboard in the web player, **Enter** is Start; see [Playing a game](/docs/server/games/#playing-a-game) for the rest.

Games that keep high scores or settings keep them on the server, one for each person.

## Coming from the Arcade plugin

Before 0.2.7, arcade games played in the web player only, through the Arcade plugin. Matinee now does this itself: when it starts, an Arcade library becomes a Games library of the same folders, and the plugin is removed. Places saved with the plugin don't carry over.
