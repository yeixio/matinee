# Writing plugins

The guide to writing Matinee plugins is on the website:
**[matinee.yeix.io/docs/plugins/overview](https://matinee.yeix.io/docs/plugins/overview/)**.
Its pages live in [guide/plugins](guide/plugins):

- [How plugins work](guide/plugins/overview.md)
- [Your first plugin](guide/plugins/first-plugin.md), a tutorial
- [plugin.json](guide/plugins/manifest.md)
- [The server part](guide/plugins/server.md), including plugins in languages other than Go
- [The player](guide/plugins/player.md)
- [Testing and debugging](guide/plugins/testing.md)
- [Publishing](guide/plugins/publishing.md)

The SDK is [pluginsdk](../pluginsdk), the `matinee-plugin` tool is
[cmd/matinee-plugin](../cmd/matinee-plugin), and
[pluginsdk/examples/stories](../pluginsdk/examples/stories) is a small
complete plugin. The [Arcade plugin](../plugins/arcade) uses every part.
