# Security policy

## Supported versions

Security fixes go into the latest tagged release and `main`. Older tags are not maintained as a separate support line.

## Reporting a vulnerability

Please don't report security vulnerabilities in public issues.

Use GitHub's private vulnerability reporting for this repository:

1. Open the repository's **Security** tab.
2. Select **Report a vulnerability**.
3. Include the affected version, steps to reproduce, what you expected and saw, and the likely impact.

Remove passwords, session cookies, API keys and private URLs from anything you paste. We'll reply in the advisory, and may ask for more detail while we work on a fix. Please give us a reasonable time to fix it before disclosing publicly. There is no bug bounty.

## Sensitive areas

These deserve extra care in review:

- **Sign-in and sessions** (`internal/auth`, `internal/api`): password hashing, session cookies, admin-only routes.
- **The folder picker** (`GET /api/fs`): admin only, and must never become a way to read arbitrary files.
- **Serving files** (direct play, artwork, subtitles, HLS segments): an item id must never resolve to a path outside its library or the data directory.
- **FFmpeg arguments**: file names and user input must never be able to inject options.
- **Secrets**: the TMDB key and database credentials stay out of logs and API responses.
