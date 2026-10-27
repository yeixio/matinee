// Adds "media server" to every guide page's <title>, so search results say
// what Matinee is. The header keeps the plain name.
import { defineRouteMiddleware } from '@astrojs/starlight/route-data'

export const onRequest = defineRouteMiddleware(({ locals }) => {
  const route = locals.starlightRoute
  const title = route.head.find((tag) => tag.tag === 'title')
  if (title) title.content = `${route.entry.data.title} | Matinee media server`
})
