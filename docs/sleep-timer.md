# Sleep timer

Every Matinee player has a sleep timer, for falling asleep to a film or a
show. This page is the shared behaviour for the web player and the apps
(web `src/player/sleep.ts`, apps `src/sleep.ts`). It needs nothing from the
server.

## Choices

- **Off**, the default.
- **15, 30, 45, 60 or 90 minutes** from when it's picked. Picking a time
  again starts it over from then. The time runs on the clock, whether or
  not the video is paused.
- **End of this episode** (or movie, or whatever's playing).

## What happens

- **A set time:** over the last 30 seconds the volume fades to nothing, then
  the player pauses where it is (so the position is saved as usual), puts
  the volume back, and turns the timer off. The player says it was paused by
  the sleep timer until someone presses play. The apps stop keeping the
  screen awake, so a phone locks and a TV's screen saver comes on.
- **End of what's playing:** auto-play offers nothing (no countdown, no
  "You might like"). When the video ends, the timer turns off and the player
  ends as it does with auto-play off: the episode is marked watched and the
  player closes.

## How long it lasts

The timer belongs to the player, not to one episode: when auto-play moves
on to the next episode it keeps running there. Leaving the player any other
way turns it off. It isn't saved anywhere, so it never follows a person to
another device.

## Where it is

- **Web:** a moon button in the controls bar, with the time left next to it
  while a set time runs. Its menu lists the choices with the current one
  ticked.
- **Phone and tablet:** a moon button in the bar above the video, next to
  the title, which opens the choices in a sheet. It shows with the system's
  own controls too.
- **TV:** pressing down while the player's controls show opens the choices;
  the controls show a moon with the time left, or "Stops at the end".
