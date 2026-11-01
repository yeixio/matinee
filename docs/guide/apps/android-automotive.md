---
title: Cars with Android built in
description: Listen to your music, audiobooks and podcasts on a car that has Google built in, with no phone needed, signed in to your server from the car itself.
since: v0.1.9
sidebar:
  order: 5
---

Some cars run Android themselves, often called "Google built-in" (Android Automotive OS). On these, Matinee installs on the car itself, from the Google Play Store in the car, and plays your music, audiobooks and podcasts with no phone at all. Videos never play in the car.

If your car shows your phone's apps when you plug the phone in instead, that's Android Auto: see [Android Auto](/docs/apps/android-auto/).

This needs Matinee version 1.0.22 or later. It's the same Matinee listing on Google Play as the phone app.

## Getting started

1. Park the car. Signing in only works while it's parked.
2. Open the **Play Store** in the car, find **Matinee** and install it.
3. Open **Matinee** from the car's media apps and choose **Sign in**.
4. Pick your server from **Servers on this network** if the car is on your home Wi-Fi. Otherwise type your server's address, the one you use away from home, and choose **Next**.
5. The car shows a code. On your phone or computer, go to the address the car shows (your server's `/link` page), or scan the QR code with your phone's camera, and approve the car. See [Signing in a device](/docs/apps/signing-in/).

The car signs in by itself a moment later. It shows up in **Settings → You → Your devices** under the car's name, where you can sign it out.

The car signs in as you, not as the shared Home account, so it works wherever the car is. Away from home it uses your server's away-from-home address, and finds the server again if that address changes (see [Watching away from home](/docs/server/remote-access/)).

## What's in the car

The car shows the same tabs as [Android Auto](/docs/apps/android-auto/#whats-in-the-car): **Listen now**, **Music**, **Audiobooks** and **Podcasts**, with the same search, the same buttons for books and episodes, and the same "Play … on Matinee" by voice through the car's assistant. Books and episodes pick up where you left off on any other device, and the car saves your place every few seconds.

Downloads are a phone feature, so the car always plays from your server. Where the signal drops, it picks up again by itself once it's back.

## Signing out, or into another server

With the car parked, open Matinee's settings from the car's media screen and choose **Sign out**. Then sign in again, to the same server or another one.

## Something isn't working

- **"Sign in to your Matinee server to listen in this car"**: the car isn't signed in, or its sign-in was removed in Your devices. Park, choose **Sign in** and sign in again.
- **"Can't reach a Matinee server there"**: the address you typed doesn't lead to your server from the car's connection. Use the address that works away from home, and check that remote access is set up.
- **"Can't reach the server"** while listening: the car can't reach your server from where it is right now. It keeps trying, and picks up again when it can.
- **Matinee isn't in the car's Play Store**: not every car with Android built in offers every app. The car's maker decides which apps its Play Store shows.
