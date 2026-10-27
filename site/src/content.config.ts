import { docsLoader } from '@astrojs/starlight/loaders'
import { docsSchema } from '@astrojs/starlight/schema'
import { defineCollection, z } from 'astro:content'

export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    // since: the release a page's feature arrived in (see scripts/prepare.mjs).
    schema: docsSchema({ extend: z.object({ since: z.string().optional() }) }),
  }),
}
