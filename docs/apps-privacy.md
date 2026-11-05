# Privacy policy for the Matinee apps

This policy covers the Matinee apps for iPhone, iPad, Apple TV, Apple Watch, Android phones and tablets, Android TV and Google TV, Wear OS, Fire TV, and Samsung and LG TVs, published by YEIXIO LLC. Last updated November 5, 2026.

## The short version

The Matinee apps talk to your own Matinee server. They ask our servers at yeix.io only two things, and only by your Matinee server's random ID: where to find your server when you're away from home (if it uses Matinee Connect), and, on Samsung and LG TVs, whether Matinee is unlocked. We don't collect, see, sell or share your data. There's no account with us, no ads, no analytics and no tracking.

## What the apps do with your data

- **Your server.** The app connects to the Matinee server you choose. Your sign-in, what you watch, your progress, your watch list and your profile settings go to that server and stay there. Whoever runs the server, usually you, controls that data. The Matinee server is open source, so you can read exactly what it keeps: https://github.com/yeixio/matinee.
- **On your device.** The app keeps your servers' addresses and sign-in tokens in the device's secure storage (the iOS keychain or Android's encrypted storage), and keeps downloads you make for watching offline. Signing out or removing the app deletes them.
- **Finding your server.** On your home network the app looks for Matinee servers with Bonjour (mDNS). That lookup stays on your local network.
- **Trailers.** When your server's admin has online trailers turned on and you choose to watch one, the trailer opens in YouTube, whose own privacy policy applies. Trailers from your own files play from your server.
- **Away from home.** If your server uses Matinee Connect, the app asks connect.matinee.yeix.io for your server's current address, by the server's ID, when it can't reach the server directly. The answer is only an address; your library and what you watch never pass through our servers.
- **Purchases.** The App Store, Google Play and the Amazon Appstore handle purchases there. We get sales reports from them that don't identify you.
- **Samsung and LG TVs.** The app is free in their stores, with a free trial. Unlocking it is a payment on yeix.io, handled by Stripe, which keeps the payment details and sends us your email address for the receipt. To know whether it's unlocked, the TV app sends your Matinee server's ID to yeix.io when it starts and while its unlock screen is open. We look the ID up among payments and don't keep anything about your TV.

## What we collect

Nothing from the apps beyond your server's ID, as above. The apps have no analytics, crash reporting, advertising or tracking code. Like any website, our servers see the IP address a request comes from, and their standard logs record requests for a short time; we don't use them to identify you or share them. If you pay on yeix.io, we keep the payment record Stripe gives us (your email address, the server's ID and the amount), as the law requires for sales.

## Children

The apps don't collect data from anyone, children included. Kids profiles on your server limit what a child can see; they're set up by the server's admin.

## Changes and contact

If this policy changes, the new version will be published here, with the date at the top. Its history is in this repository.

Questions: hello@yeix.io, or open an issue at https://github.com/yeixio/matinee/issues.
