# Translations

Matinee's text in every language: the web player, the server's error
messages, and the phone and TV apps, which copy this folder when they build.

```text
i18n/
  languages.json          languages people can pick, with their direction and status
  i18n.go                 the list for the server, and how it matches a device's language
  locales/<language>/     one folder per BCP 47 tag, such as en, pt-BR or zh-Hant
    common.json           words every client shows: genres, credits, awards, durations
    web.json              the web player's own text
    errors.json           what each error code from the server means
```

English (`en`) is the source. Every other language has the same files and
keys. A key that is missing falls back to English, so a language can ship
before it is complete if `languages.json` marks it `partial`.

## Languages

English, German (`de`), Spanish (`es`), French (`fr`), Italian (`it`),
Brazilian Portuguese (`pt-BR`), Dutch (`nl`), Polish (`pl`), Swedish
(`sv`), Norwegian Bokmål (`nb`), Danish (`da`), Finnish (`fi`), Czech
(`cs`), Greek (`el`), Hungarian (`hu`), Romanian (`ro`), Turkish (`tr`),
Russian (`ru`), Ukrainian (`uk`), Indonesian (`id`), Malay (`ms`),
Vietnamese (`vi`), Thai (`th`), Hindi (`hi`), Japanese (`ja`), Korean
(`ko`), Simplified Chinese (`zh-Hans`), Traditional Chinese (`zh-Hant`),
Arabic (`ar`), Hebrew (`he`), Persian (`fa`) and Urdu (`ur`). Arabic,
Hebrew, Persian and Urdu are written right to left, and the web player
mirrors its layout for them.

All but English are machine translated (`status: machine`). Settings says
so under the language picker, with a link here, until a person reviews one
and its status becomes `reviewed`.

Each profile picks its language in Settings (`PUT /me/language`), and it
follows them to every device. Until it picks one, each device uses its own
languages: exactly (pt-BR), by base language (de-AT → de), by script (zh-TW
→ zh-Hant), or by another region (pt-PT → pt-BR), never Simplified for
Traditional Chinese. The server matches the same way (`i18n.Match`) and
fetches titles, descriptions and trailers from TMDB in that language where
TMDB has them.

## Reviewing a translation

Read it in the web player, fix wording in `locales/<tag>/*.json`, and set
the language's `status` to `reviewed` in `languages.json` in the same pull
request. Keep a term the same everywhere it appears, and keep the tone of
the English: short, plain, and talking to one person.

## Keys

- Keys are stable names, never the English text: `nav.settings`, not
  `"Settings"`. Changing the English wording keeps the key.
- Placeholders use `{{name}}` and must match English in every language. A
  plural form that stands for a single number, such as Arabic's `_two`, may
  spell the number out instead of using `{{count}}`.
- Plurals use i18next suffixes: `key_one` and `key_other` in English, and
  `_zero`, `_two`, `_few` or `_many` where a language needs them. Every form
  the language uses must be there; a test checks it.
- `<name>`, `<code>`, `<url>` and `<link>` are markup the web player fills
  in. Keep them around the same words.
- Matinee, TMDB, Radarr, Sonarr, YouTube and API field names stay as they
  are.

## Errors

The server sends each error with a stable `code`, such as
`no_such_library`, and the English `error` message, with `params` for any
names in it. Clients show `errors:<code>` from the catalog, filled in from
`params`, and fall back to `error`. `errors.json` in English is generated
from the server's messages; after adding or changing one, run:

```sh
go test ./internal/api -run TestErrorCatalog -update-errors
```

and translate the new keys in every language.

## Adding a language

1. Add it to `languages.json` with its name in itself, its direction, and
   `status: machine` or `partial`.
2. Copy `locales/en/` to `locales/<tag>/` and translate it.
3. Run the web tests. `catalog.test.ts` checks keys, placeholders and plural
   forms, and fails until every listed language has a folder.

Vitest caches the list of catalog files, so after adding a folder clear
`web/node_modules/.vite` and `web/node_modules/.vitest-cache` if the tests
don't see it.

In development the picker also offers two pseudo-languages: `en-XA`, which
stretches every string to find text that doesn't fit, and `ar-XB`, which
mirrors the layout to find what breaks right to left.
