# Themes

A theme sets Matinee's colours and fonts in the web player and in the phone
and TV apps. Everyone picks their own under **Settings → Appearance**, and
their pick follows them to every device. An admin also picks the **server
theme**, which people see until they pick their own, and on the sign-in page.

Matinee ships with seven themes:

| Theme | Look |
|---|---|
| Matinee | Warm theatre dark with a coral accent. The default. |
| Dark | True black and crisp white, easy on an OLED screen. |
| Light | Warm paper white with coral, for a bright room. |
| Clean | Plain white, one blue, no serifs. |
| Sci-fi | Deep space navy, neon cyan glow, Orbitron headings. |
| Drive-in | Seventies browns and marquee amber. |
| High contrast | Black, white and bright yellow, for the clearest text. |

A theme is a small JSON file of colours and font names. It holds no code and
no CSS, so it is safe to share and to install from someone else.

## Add your own theme to a server

1. Make a `themes` folder in the server's data folder (`MATINEE_DATA_DIR`,
   `./data` by default; `/data` in the Docker image and the Helm chart).
2. Save a theme file in it, such as `themes/mint.json`:

   ```json
   {
     "id": "mint",
     "name": "Mint",
     "description": "Cool green on near-black.",
     "author": "your-name",
     "scheme": "dark",
     "fonts": { "display": "inter", "body": "inter" },
     "colors": {
       "ink": "#0b1210",
       "surface": "#101a17",
       "raised": "#18241f",
       "line": "#25352f",
       "text": "#eafff6",
       "muted": "#9cc4b4",
       "faint": "#6c8e81",
       "accent": "#3ddc97"
     }
   }
   ```

3. Open **Settings → Appearance**. The theme is listed straight away, with no
   restart; the apps pick it up the next time they open. If the file has a
   mistake, admins see it under **Server theme** (for example
   `Skipped mint.json: colors.accent is required`), and the server log says so
   at startup.

On Kubernetes, mount a ConfigMap of theme files at `/data/themes`.

## The theme file

| Field | Required | What it is |
|---|---|---|
| `id` | yes | 1-40 lowercase letters, digits and dashes. Must be unique, and can't be a built-in theme's id. |
| `name` | yes | What the picker shows, up to 40 characters. |
| `description` | no | One line under the name in the picker. |
| `author` | no | Who made it. |
| `scheme` | yes | `dark` or `light`: what the system's own parts (scroll bars, keyboards, alerts, the status bar) should match. |
| `fonts.display` | no | Titles and headings: `fraunces` (default), `inter` or `orbitron`. |
| `fonts.body` | no | Everything else: `inter` (default). |
| `colors` | yes | See below. |

Fonts come from the set every app ships, since a TV can't download a font
from a theme. Adding a typeface means adding it to the web player and the
apps (see [Add a built-in theme](#add-a-built-in-theme-to-matinee)).

### Colours

Write colours as `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb(r, g, b)` or
`rgba(r, g, b, a)`. Colour names and anything else are refused.

The first eight are required. The rest are worked out from them when left
out, so most themes only need the eight.

| Colour | Required | Used for | When left out |
|---|---|---|---|
| `ink` | yes | The page, behind everything. | |
| `surface` | yes | Panels, bars, the TV's menus. | |
| `raised` | yes | Cards, inputs and buttons on a panel; artwork while it loads. | |
| `line` | yes | Borders and dividers. | |
| `text` | yes | Titles and body text. | |
| `muted` | yes | Secondary text: years, runtimes, descriptions. | |
| `faint` | yes | Hints and the least important text. | |
| `accent` | yes | The Play button, links, progress bars, what has focus on a TV. | |
| `accentStrong` | no | The accent when hovered or pressed. | `accent` |
| `onAccent` | no | Text and icons on an accent button. | `ink` or `text`, whichever reads better on `accent` |
| `danger` | no | Errors and deleting. | a soft red that suits the scheme |
| `glow` | no | The halo around what has focus on a TV. | `accent` at 45% |
| `glass` | no | Frosted buttons over artwork on a TV. | `text` at 14% (8% for light themes) |
| `glassFocused` | no | A frosted button with focus. | `text` at 30% (16% for light themes) |
| `scrim` | no | Behind the video player. | black |
| `track` | no | A progress bar's unplayed part. | `text` at 25% (20% for light themes) |
| `shade` | no | The dark wash under text on artwork. | black at 55% |
| `veil` | no | Tints a backdrop towards the page. | `ink` at 35% |

The video player itself stays dark in every theme, since it sits on the
video.

### Making it readable

From across a room, contrast matters more than on a laptop. Aim for WCAG AA:

- `text` on `ink` and on `raised`: at least 4.5:1
- `muted` on `ink` and on `surface`: at least 4.5:1
- `faint` on `ink`: at least 3:1
- `onAccent` on `accent`: at least 4.5:1
- `accent` on `ink`: at least 3:1 (it marks what has focus)

The built-in themes all pass; `go test ./internal/theme` checks them. For your
own file, any contrast checker works, or put it in
`internal/theme/builtin/` and run that test.

Tips:

- On a TV, focus is shown by a scale, the `accent` outline and the `glow`.
  If your accent is close to your `raised` colour, focus is hard to see.
- Light themes: keep `ink` slightly off-white (`#f7f3ee`, not `#ffffff`) and
  use a `raised` that differs from it, so cards stand out.
- Look at the theme on the TV app's home screen and a movie's page, and on
  the web player's home, a movie's page and Settings.

## Add a built-in theme to Matinee

Built-in themes ship with every server and are bundled in the apps, so they
work before the apps reach a server. To contribute one:

1. Add `internal/theme/builtin/<id>.json` here and add its id to
   `builtinOrder` in `internal/theme/theme.go`, where it should show.
   `go test ./internal/theme` checks the file and its contrast.
2. In [matinee-apps](https://github.com/yeixio/matinee-apps), copy the same
   file to `mobile/src/themes/<id>.json` and list it in
   `mobile/src/themes/index.ts`. `pnpm test` there checks the apps' copy
   matches the format.
3. Open a pull request in each repository, with a screenshot of the web home
   page and the TV home page in the theme.

A new typeface also needs adding to `DisplayFonts` or `BodyFonts` in
`internal/theme/theme.go`, to `fontStacks` in `web/src/theme.ts` (with its
`@fontsource-variable` package imported in `web/src/styles.css`), and to the
apps' `mobile/src/themes/fonts.ts` (with its `@expo-google-fonts` package).
Use fonts under the SIL Open Font License or similar.

## For client developers

- `GET /api/v1/themes` (no sign-in needed) lists every theme with all of its
  colours filled in, written as `#rrggbb` or `rgba(r,g,b,a)`, and the
  server's default.
- `GET /api/v1/me/theme` and `PUT /api/v1/me/theme` read and save the
  current person's pick (`{"theme": "sci-fi"}`; `""` follows the server).
  On the home network without signing in, that's the shared Home account.
- Admins set the server theme with `PUT /api/v1/settings` and
  `{"theme": "light"}`.

The web player turns a theme into the CSS variables in `web/src/styles.css`
(`--color-ink`, `--color-accent-strong`, ...), which the Tailwind classes
(`bg-ink`, `text-muted`) read. Anything on the page should use those tokens,
not `white` or `black`, or it will be wrong in light themes. The apps read
colours through `useTheme()` in `mobile/src/theme.tsx`.
