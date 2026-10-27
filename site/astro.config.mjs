// The Matinee website: the landing page (src/pages/index.astro) and the
// user guide, which is written in docs/guide at the top of the repository.
// scripts/build-all.mjs builds the guide once per release, each at its own
// base path; `pnpm dev` and `pnpm build` build the working tree's guide.
import starlight from '@astrojs/starlight'
import { defineConfig } from 'astro/config'
import { readFileSync } from 'node:fs'

const base = process.env.SITE_BASE ?? '/'
// Which docs this build is: a release tag, "next" for main, or "dev".
const version = process.env.DOCS_VERSION ?? 'dev'
// The guide's own sidebar, so each release keeps the order it was written in.
const sidebar = JSON.parse(readFileSync(new URL('./src/content/docs/docs/sidebar.json', import.meta.url), 'utf8'))

export default defineConfig({
  site: process.env.SITE_URL ?? 'https://matinee.yeix.io',
  base,
  trailingSlash: 'always',
  vite: {
    define: {
      'import.meta.env.DOCS_VERSION': JSON.stringify(version),
      'import.meta.env.DOCS_LATEST': JSON.stringify(process.env.DOCS_LATEST ?? ''),
    },
  },
  integrations: [
    starlight({
      title: 'Matinee',
      description: 'The guide to Matinee, a free, open source, self-hosted media server for your movies, TV and music.',
      logo: { src: './public/favicon.svg', alt: '' },
      favicon: '/favicon.svg',
      customCss: ['@fontsource-variable/inter', '@fontsource-variable/fraunces', './src/styles/docs.css'],
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/yeixio/matinee' }],
      lastUpdated: false,
      sidebar,
      components: {
        SiteTitle: './src/components/SiteTitle.astro',
        Banner: './src/components/VersionBanner.astro',
      },
      routeMiddleware: './src/routeData.ts',
      head: [
        { tag: 'meta', attrs: { name: 'docs-version', content: version } },
        // Only the latest release's guide at / belongs in search results. Main's
        // guide at /next/ and each release's copy at /<tag>/ would compete with it.
        ...(base === '/' ? [] : [{ tag: 'meta', attrs: { name: 'robots', content: 'noindex, follow' } }]),
      ],
    }),
  ],
})
