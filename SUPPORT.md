# Getting help with Matinee

Matinee is a free, open-source media server, and the Matinee apps for iPhone, iPad, Apple TV, Android and Android TV play from it. Help for both is here.

## Setting up the server

The [README](README.md#run-it) walks through installing the server with Docker, a package or a single download, adding your movies and shows, and getting posters and descriptions.

## Connecting an app

1. Install and start the server, and open it in a browser once to create the admin account.
2. Open the Matinee app on a device on the same network. It finds the server on its own; if it doesn't, type the address you open Matinee at in a browser, such as `http://192.168.1.20:7171`.
3. Sign in with your username and password, or choose **Sign in with a code** and type the code at `<your server>/link` on a computer or phone that's already signed in.

To watch away from home, see Settings → Remote access on the server.

## Something's wrong

- **Search the [issues](https://github.com/yeixio/matinee/issues)**: someone may have hit it already.
- **Open a new issue** with what you did, what you expected, what happened, the app and server versions (Settings in the app shows both) and the device. Leave out passwords, tokens and private addresses.
- **Security problems** go through private reporting instead; see [SECURITY.md](SECURITY.md).
- **Purchases and refunds** are handled by the App Store or Google Play.

You can also email hello@yeix.io.

## Privacy

The apps talk only to your own server. See the [privacy policy for the apps](docs/apps-privacy.md).
