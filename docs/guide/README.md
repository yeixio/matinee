# The Matinee user guide

These pages are the user guide on the Matinee website. The site is built from
`site/` and publishes a copy of this guide for every release, so what's here
on `main` becomes the next release's guide.

- Write for someone running Matinee at home, not for developers.
- Each page has front matter with a `title`, a `description` and a
  `sidebar.order`. `sidebar.json` lists the sections.
- A page about a feature that isn't in a release yet gets `since: vX.Y.Z`
  (the first release that will have it), so the guides for older releases
  leave it out.
- Screenshots live in `images/` and are made by `site/demo/shoot.mjs` from a
  real server running a made-up library. Run `pnpm shots` in `site/` after a
  change to a screen the guide shows; don't take them by hand.

Preview the site with `cd site && pnpm install && pnpm dev`.
