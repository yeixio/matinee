---
title: Notifications on your phone
description: Get Box Office's news as your phone's or tablet's own notifications, even with Matinee closed, straight from your own server.
since: v0.2.3
sidebar:
  order: 4
---

When your server uses [Box Office](/docs/server/radarr-and-sonarr/#box-office), Matinee tells you when something you asked for is found and downloading, and when it's ready to watch. Admins also hear about downloads that fail or look unsafe. Inside the app, a banner shows these while Matinee is open, and the bell on Home lists them.

On an iPhone, iPad, or Android phone or tablet, they can also arrive as the device's own notifications, on the lock screen, with Matinee closed. This needs app version 1.0.30 or later. There's nothing to set up on the server beyond connecting Box Office.

## Turning them on

1. Open **Settings** in the app and find **Notifications**. It shows once your server sends notifications, and you're signed in as yourself.
2. Turn on **Notify me on this device**, and allow notifications when the device asks.

The bell's list also offers **Turn on** at the top until you do. Each phone or tablet has its own switch, so turning it on here doesn't change your other devices.

Tap a notification to open the title, once it's in your library, or the list otherwise. Opening the list clears the ones you've seen from the lock screen.

If you said no when the device asked, the switch says notifications are off in the device's settings. Tap **Open settings**, turn on notifications for Matinee there, and come back.

## How quickly they arrive

Your phone asks your own Matinee server for news in the background. Nothing goes through any other server, so what Box Office says stays between your server and your devices.

- **Android:** about every 15 minutes, a little longer when the battery saver is on.
- **iPhone and iPad:** whenever iOS lets apps check in the background. It learns from how often you use Matinee, so it can take a while, sometimes hours, and it stops if you swipe Matinee away in the app switcher. Opening the app shows everything new at once.

While Matinee is open, news shows straight away as a banner, as before.

## Who gets them

- Notifications go to the person who asked for the title, and to admins as chosen under **Settings → Add-ons → Radarr and Sonarr** on the server.
- Kids profiles and the shared Home account (watching at home without signing in) don't get any.
- If several people use the same phone, only whoever is signed in there gets notifications on it. Signing in as someone else starts fresh.

## On a TV

Apple TV, Android TV and Google TV don't show notifications from apps, so the TV apps show the banner while they're open and keep the rest in the bell's list.
