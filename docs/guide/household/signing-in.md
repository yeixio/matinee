---
title: Who has to sign in
description: When Matinee asks for a password and when it doesn't, at home and away, and what keeps your server safe when it's reachable from the internet.
sidebar:
  order: 2
---

Matinee is meant to be easy at home and closed to everyone else. In short: devices at home can watch without an account, and anything coming from outside your home has to sign in, every time, with no exceptions.

| Where the device is | Without signing in | Signed in |
| --- | --- | --- |
| On your home network | Browse and play as the shared **Home** account | Everything that account may do |
| Away from home | Nothing: only the sign-in screen | Everything that account may do |
| At home, with **Ask for sign-in at home** on | Nothing: only the sign-in screen | Everything that account may do |

## At home

A phone, TV or browser on your home network can browse and play without an account. They all use one shared **Home** account, which can only look and play: it can't change settings, add or approve devices, download, ask for movies and shows, or see anyone else's account. Its watch progress is shared by everyone who uses it, unless you give it [profiles](/docs/household/profiles/#profiles-at-home).

A profile picked at home this way only works at home. If that phone leaves the house, it has to sign in.

To make everyone sign in at home too, turn on **Settings → Home network → Ask for sign-in at home**. Until the first admin account exists, nobody gets in as Home: setting up the server comes first.

## How Matinee decides you're at home

Matinee goes by the address a connection comes from, and when it can't be sure, it treats the device as away:

- A device with a home network address (such as `192.168.x.x`) connecting straight to the server counts as home.
- A request that came through a reverse proxy, ingress or tunnel never counts as home, even from inside the house, because a proxy makes internet traffic look local too.
- Nothing that arrives through the [remote access](/docs/server/remote-access/) port counts as home.
- With a separate home port (`MATINEE_HOME_ADDR`, which the Kubernetes chart opens for you), only connections to that port count.

[Your home network](/docs/server/home-network/#how-matinee-knows-youre-at-home) covers setting this up behind a proxy, in Docker or on Kubernetes.

## Away from home

Away from home, every device signs in with a username and password, or with a code (below). Until then the server answers only what's needed to sign in: its name, its sign-in page, and the code sign-in. Your library, posters, video and settings all need a signed-in account.

When Matinee opens a port for [remote access](/docs/server/remote-access/), that port only speaks HTTPS, with a certificate from Let's Encrypt, so passwords and video are encrypted on the way. Admins choose who has an account under [Settings → People](/docs/household/people/); nobody can make one for themselves.

<!-- since: v0.1.1 -->
### Matinee Connect never carries your video

[Matinee Connect](/docs/server/remote-access/#matinee-connect) only helps the apps find your server's current address. The apps then connect straight to your server and sign in there, as above. Connect never sees your library, your accounts, your passwords or anything you watch, and nothing streams through it: there is no relay. It can't let anyone into your server, because getting in still takes an account on the server itself.

Before an app uses an address it got from Connect, the server there has to prove it's yours: it signs a random challenge with a key that never leaves your server, and the app checks the signature against the server it paired with at home. So even someone who took over Connect, or who ends up with your old internet address, can't pose as your server to collect sign-ins.
<!-- /since -->

## Signing in a TV or other device with a code

Typing a password with a remote is no fun, so TVs and other devices can show a short code instead. Someone already signed in opens `<your server>/link` on a phone or computer, types the code and approves it, and the device is signed in as them. [Signing in a device](/docs/apps/signing-in/) shows the steps.

- A code only works for 10 minutes.
- Only a signed-in account can approve one. The Home account and kids' profiles can't, so a device at home can't approve itself.
- Check the device name on the approval page before you approve: approving signs that device in as you.

## Staying signed in, and signing out

A device stays signed in as long as it's used at least once every 90 days, so a TV or [Home Assistant](/docs/server/home-assistant/) doesn't keep asking.

**Settings → Your devices** lists every device signed in as you. Sign out any you don't recognize, or one you've lost. Changing your password signs out all your other devices, and an admin [resetting someone's password](/docs/household/people/#manage-accounts) signs that person out everywhere.

## Who can control which player

Every open browser player and app can be seen and controlled through the server, which is how [Home Assistant](/docs/server/home-assistant/) pauses a film or starts an album. Who may do that depends on who is signed in:

| Who | Their players can be controlled | They can control |
| --- | --- | --- |
| A device at home, not signed in (Home) | yes, by admins | nothing |
| Someone signed in | yes, by themselves and admins | their own account's players, including its profiles |
| An admin, or Home Assistant | yes | every player on the server |

Starting something on a player plays it as whoever uses that player, so a kids profile only gets what it could pick itself. A player that's being controlled says so for a moment, such as **Controlled from Home Assistant**.

<!-- since: v0.1.1 -->
The music player takes the same commands as the video player: play, pause, skip to the next or previous song, move around the song, and start an album, an artist or a song, which plays in the music player while the person keeps browsing.

<!-- /since -->

<!-- since: v0.1.1 -->
## Guessing passwords doesn't work

After 5 wrong passwords for one account, Matinee makes whoever is trying wait before the next try: 30 seconds at first, then twice as long after each wrong one, up to 15 minutes. One internet address gets 20 wrong passwords across all accounts before it has to wait too. A right password starts the count over.

At home nothing counts and nobody waits, so someone guessing from outside can never lock your household out: sign in at home and the account is clear again.
<!-- /since -->

## Keeping your server safe

- Give every account a password you don't use anywhere else. Matinee asks for at least 8 characters; longer is better, especially for admins, once remote access is on.
- Only make admins of people who need to change settings.
- If something in front of Matinee rewrites addresses, such as a router or port forward that makes internet traffic look like it comes from inside your home, turn on **Ask for sign-in at home**.
- Remote access is off until an admin turns it on. If nobody watches away from home, leave it off.
