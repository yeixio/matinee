---
title: The phone and TV apps
description: The Matinee apps for iPhone, iPad, Android, Apple TV, Android TV and Google TV, how they find your server, and what they can do.
sidebar:
  order: 1
---

The Matinee apps bring your library to phones, tablets and TVs, with the device's own video player so most files play exactly as they are.

<div class="pair">

![Home in the phone app (phone)](../images/app-phone-home.webp)

![A movie in the phone app (phone)](../images/app-phone-details.webp)

</div>

| App | Runs on |
| --- | --- |
| Matinee for phones and tablets | iPhone, iPad, Android phones and tablets |
| Matinee for TV | Apple TV, Android TV and Google TV |

<!-- since: v0.1.1 -->
On any other TV with AirPlay or Chromecast, [cast from your phone](/docs/apps/casting/).
<!-- /since -->

The apps are a one-time purchase in the App Store and Google Play. The server stays free and open source; buying an app is how the project is funded.

## Connecting to your server

On your home network, open the app and it finds Matinee by itself: the server announces itself on the network, so nobody needs to know its address. With one server, the app goes straight to it. With several, it lists them.

If the app doesn't find it (for example, Matinee runs in Docker without host networking), type the address, such as `192.168.1.20:7171` or `https://matinee.example.com`. [Your home network](/docs/server/home-network/) explains discovery.

## At home and away

At home, the apps can watch [without signing in](/docs/server/home-network/#watching-without-signing-in), as the server's shared Home account. Sign in for your own watch history, watch list and downloads, and to watch away from home. Once you're signed in, the app uses the server's home address at home and its outside address (from [remote access](/docs/server/remote-access/) or your own domain) away, switching by itself.

On a TV, sign-in starts with a code instead of a password: see [Signing in a device](/docs/apps/signing-in/).

## What's in the apps

- **Home, libraries, search, and every title's page**, as in the browser, with cast and crew, trailers, file details and the [filters](/docs/watching/browsing/#filters).
- **Your profile and theme.** The [profile picker](/docs/household/profiles/) and [your theme](/docs/household/themes/) follow you from the server.
- **Playback that picks up where you left off**, with the [Up next](/docs/watching/playing/#up-next) countdown, picture in picture on phones, and music that keeps playing in the background.
<!-- since: v0.1.1 -->
- **Music on phones** with its own player: artists, albums and songs, a player above the tabs that keeps going while you browse, and a full player with the queue. See [Music](/docs/watching/music/#on-the-phone-apps).
<!-- /since -->
<!-- since: v0.1.3 -->
- **Music on a TV** too: the library's tabs, album and artist pages, a player with the queue, and music that keeps playing while you browse, with what's playing at the top of the screen. See [Music](/docs/watching/music/#on-a-tv).
- **The music visualizer** on phones and TVs, in time with the song. See [The visualizer](/docs/watching/music/#the-visualizer).
<!-- /since -->
<!-- since: v0.1.2 -->
- **Audiobooks** on phones with a player made for books: chapters, back and ahead, the speed and a sleep timer that can stop at the end of a chapter. See [Audiobooks](/docs/watching/audiobooks/#in-the-apps).
<!-- /since -->
<!-- since: v0.1.5 -->
- **Audiobooks on a TV** with the same kind of player: the chapters in a row, back 15 and ahead 30 seconds, the speed and the sleep timer, and the book playing on while you browse. See [Audiobooks](/docs/watching/audiobooks/#on-a-tv).
<!-- /since -->
- **Downloads** on phones and tablets, to [watch offline](/docs/apps/downloads/).
<!-- since: v0.1.5 -->
- **Android Auto**: music and audiobooks in the car, from the car's screen, its buttons or by voice. See [Android Auto](/docs/apps/android-auto/).
- **Wear OS watches**: a remote on your wrist for whatever Matinee plays on your phone, TV or computer, with a tile and a complication. See [Wear OS watches](/docs/apps/wear-os/).
<!-- /since -->
<!-- since: v0.1.6 -->
- **Apple Watch**: the same remote on your wrist, with a complication for your watch face. See [Apple Watch](/docs/apps/apple-watch/).
<!-- /since -->
<!-- since: v0.1.7 -->
- **CarPlay**: music and audiobooks in the car, from the car's screen and its buttons. See [CarPlay](/docs/apps/carplay/).
- **Podcasts** on phones and TVs: follow shows, add new ones, and listen in the audiobook player with show notes. See [Podcasts](/docs/watching/podcasts/#in-the-apps).
- **Siri and Google Assistant**: "play … on Matinee" for music, audiobooks and playlists, with the app closed. See [Siri and Google Assistant](/docs/apps/voice/).
<!-- /since -->
<!-- since: v0.1.9 -->
- **Your star ratings** on phones and TVs, from app version 1.0.21: rate titles, albums, artists, audiobooks and podcasts, and sort a library by **My rating**. See [Your ratings](/docs/watching/title-pages/#your-ratings).
<!-- /since -->
- **Coming soon**, with [Radarr or Sonarr](/docs/server/radarr-and-sonarr/#coming-soon) connected: a row at the end of Home, and the next two months day by day. On a phone, the calendar along the top jumps to a day; on a TV, each day is a row.

## On a TV

The TV app has a layout made for the couch and a remote: tabs along the top, big type, and a coral outline on whatever's selected.

![The TV app's Home](../images/app-tv-home.webp)

![A movie's page in the TV app](../images/app-tv-details.webp)

On Apple TV, the **top shelf** (the banner above Matinee's icon on the home screen) shows your Continue watching, watch list and Recently added. Select one to open it, or press Play to start it.

Android TV and Google TV install the same app as Android phones, from Google Play.

<!-- since: v0.2.2 -->
### Your libraries in the top bar

From version 1.0.28, **Settings → Top bar** on the TV puts the libraries you watch most in the top bar as tabs of their own, right after Home, so Movies or Shows is one press away from anywhere. **Libraries** stays for the rest. **Open on** picks where the app starts: Home, or one of your libraries. Both are kept for each profile on that TV, and nothing is in the top bar until you add it, so it looks the same after every update.

![Settings → Top bar on a TV, with Movies in the top bar](../images/app-tv-top-bar.webp)

Back always goes one step back: from a screen to the one before, from any tab to Home, and on Home first up to the top bar. Only Back from the top bar on Home leaves the app.

<!-- /since -->

<!-- since: v0.1.4 -->
## Watching on a TV

The remote works the way it does in your TV's own player:

| Press | While something plays |
| --- | --- |
| Select, or Play/Pause | Pause, and play again |
| Left or right | Back or forward 10 seconds (hold for a minute<!-- since: v0.2.4 -->, or from app version 1.0.34 a whole chapter in a video that has [chapters](#chapters)<!-- /since -->); rewind and fast forward on remotes that have them do the same. <!-- since: v0.1.11 -->From app version 1.0.23, a picture of where you'll land shows above the bar ([seek bar previews](/docs/watching/seek-previews/)).<!-- /since --> <!-- since: v0.2.4 -->From app version 1.0.33 on Apple TV, a swipe left or right on the Siri Remote's touch surface does the same, and sliding your finger along it scrubs: the knob follows your finger with the picture there, and the video moves there when you let go.<!-- /since --> |
| Up | Show the bar: where you are, the time left and when it ends |
| Down | Show the bar and go to the buttons under it: **Audio**, **Subtitles**<!-- since: v0.2.4 -->, **Chapters**<!-- /since --> and **Sleep timer** |
| Up or Back, on the buttons | Back to the bar |
| Back | Leave the player |

<!-- since: v0.1.10 -->
While an episode's intro plays, **Skip intro** shows in the bottom corner, ready for the remote: press select to skip it, or carry on with any other button. See [Skipping intros](/docs/watching/skipping-intros/).
<!-- /since -->

<!-- since: v0.1.14 -->
**Skip credits** works the same way at the end of a movie: press select to finish it. See [Skipping credits](/docs/watching/skipping-credits/).
<!-- /since -->

![The player on a TV, with subtitles on and the buttons under the bar](../images/app-tv-player.webp)

On the buttons, left and right move between them and select opens one. **Audio** lists every sound track in the file, by language, with commentaries and audio description marked and the format of each (such as Dolby TrueHD · Atmos · 7.1). **Subtitles** has **Off** and **On**, which picks the best track in your languages and says which (from version 1.0.27). **More subtitles** lists every subtitle track in the file and the [files next to it](/docs/getting-started/organizing-media/#subtitles). The one in use has a check, and the button says what's on.

<!-- since: v0.1.10 -->
At the bottom of **More subtitles**, **Find subtitles…** looks for subtitles online when the server has [subtitle search](/docs/watching/finding-subtitles/) set up. Select a result to add it to the video and turn it on.
<!-- /since -->

![Picking the audio track on a TV](../images/app-tv-audio.webp)

<!-- since: v0.2.4 -->
### Chapters

From app version 1.0.34, a movie or episode whose file has its own chapters (most Blu-ray and DVD rips do) gets a **Chapters** button under the bar, which says the chapter playing. Select it for a row of every chapter along the bottom of the screen: a picture from a little way into each (the [seek bar previews](/docs/watching/seek-previews/), once the server has made them), its name and where it starts. The chapter playing has the remote's focus and an accent border. Left and right go along the row, select plays from that chapter, and back closes the row.

![The chapters of a film on a TV, with a picture of each](../images/app-tv-chapters.webp)

You can also skip a whole chapter without opening the list. While the bar is up or the video plays, hold left or right on the remote (on the Siri Remote, click and hold the left or right of the clickpad). Held right goes to the start of the next chapter. Held left goes back to the start of the chapter playing, or to the one before when it only started a few seconds ago, the way a CD player's back button works. Each hold adds up before the video jumps, with the chapter's name over the bar, so two holds skip two chapters. In a video without chapters, holding still skips a minute.

Chapters come from the file itself, so there's nothing to set up. Samsung and LG remotes don't send a held press, so on those TVs use the **Chapters** button.
<!-- /since -->

The app remembers the subtitle language you pick for the next episode and the next film, as the browser does. Picture subtitles from Blu-rays and DVDs, and another sound track in a file the TV can't play as it is, need the server to convert the video again from where you are, so the picture pauses for a moment.

On a phone or tablet, the audio and subtitles buttons are at the top of the player, next to the title.
<!-- /since -->

<!-- since: v0.1.1 -->
## Sleep timer

The apps have the same [sleep timer](/docs/watching/playing/#sleep-timer) as the browser: a set number of minutes, or the end of this episode or movie, then the sound fades out and playback pauses.

- **On a phone or tablet**, tap the moon at the top of the player, next to the title.
- **On a TV**, press down on the remote while something plays to reach the sleep timer, then pick a time. The moon at the bottom of the controls shows the time left.

![The sleep timer's choices on a TV](../images/app-tv-sleep.webp)

Once the timer pauses playback, the phone or TV is free to lock or go to sleep by itself.

<!-- /since -->

<!-- since: v0.1.1 -->
## Controlled from Home Assistant

While the app is open, [Home Assistant](/docs/server/home-assistant/) can see what it plays and control it: pause, seek, start a film, set the sleep timer. The app says **Controlled from Home Assistant** for a moment when that happens. On a TV, the volume stays with the TV.
<!-- /since -->
