# Governance

Matinee is maintained by YEIXIO LLC. The server and web player are free software under the AGPL. The phone and TV apps are paid and live in a separate repository; their sales fund the project.

Maintainers decide what is merged and what is released. Proposals are welcome as GitHub issues. A useful proposal says what problem it solves, who it's for, and whether it changes the HTTP API that apps rely on.

Large changes start as an issue before a large pull request. That includes new library kinds, the plugin framework, storage schema changes, and anything that changes the API. Issues labeled `needs design` are waiting for exactly that conversation, and anyone can start it.

## Roles

| Role | What you can do | How you get there |
| --- | --- | --- |
| **Contributor** | Open issues and pull requests, review other people's work. | Anyone, from the first report or pull request. |
| **Triager** | Label, assign and close issues. Ask for reproductions. Mark issues `good first issue`. | A few merged pull requests, or a steady record of useful reports and answers, plus an invitation. |
| **Reviewer** | Approve pull requests in one area, such as scanning, streaming, the web player, music, deploy or docs. A maintainer still merges. | Regular, careful contributions to that area over a few months, plus an invitation. |
| **Maintainer (core team)** | Merge, cut releases, and decide the roadmap with the other maintainers. | Invited by the current maintainers. |

Current people in each role are listed in [MAINTAINERS.md](MAINTAINERS.md). A maintainer who invites someone opens a pull request that adds them there.

Roles are about trust, not volume. Small, focused pull requests with tests, clear bug reports, and helping other people in issues all count. Nobody needs to ask for a role: the maintainers review the contributor list each release and reach out. If you'd like to be considered for the core team sooner, say so in the [core team issue (#51)](https://github.com/yeixio/matinee/issues/51).

Someone who has been inactive for six months moves to the emeritus list in MAINTAINERS.md and can come back by asking. Anyone who breaks the [Code of Conduct](CODE_OF_CONDUCT.md) can lose a role.

Contributions are covered by the [Matinee Contributor License Agreement](CLA.md). See [CONTRIBUTING.md](CONTRIBUTING.md) before sending code.
