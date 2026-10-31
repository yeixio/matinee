#!/usr/bin/env bash
# Writes the GitHub release notes for a tag to stdout: docs/release-notes/<tag>.md
# when there is one, how to install, then the merged pull requests GitHub lists.
# The Release workflow uses it for new releases, and Release notes to refresh
# a release after its notes file changes.
#
#   scripts/release-notes.sh v0.2.0 > notes.md
set -euo pipefail

tag="$1"
repo="${GITHUB_REPOSITORY:-yeixio/matinee}"
owner="${repo%%/*}"
version="${tag#v}"

if [ -f "docs/release-notes/${tag}.md" ]; then
  cat "docs/release-notes/${tag}.md"
  echo
fi

cat <<NOTES
### Install

- **Docker:** \`docker pull ghcr.io/${repo}:${version}\`
- **Kubernetes:** \`helm install matinee oci://ghcr.io/${owner}/charts/matinee --version ${version}\`
- **Homebrew (macOS, Linux):** \`brew tap yeixio/matinee https://github.com/yeixio/matinee && brew install yeixio/matinee/matinee\`
- **Debian, Ubuntu, Fedora, RHEL:** add the apt or dnf repository as the [README](https://github.com/${repo}#debian-and-ubuntu) shows, or install the \`.deb\` or \`.rpm\` below. Either runs Matinee as a service on port 7171.
NOTES
# The Mac app is attached only when it was signed (see mac.yml).
if [ -n "${MAC_APP:-}" ] || gh release view "$tag" -R "$repo" --json assets --jq '.assets[].name' 2>/dev/null | grep -qx Matinee-Server.dmg; then
  echo "- **Mac app:** download \`Matinee-Server.dmg\` below and drag Matinee Server to Applications. It runs the server from the menu bar and updates itself (macOS 13 or later)."
fi
cat <<NOTES
- **Single binary:** download the archive for your system below. FFmpeg must be on the PATH.
NOTES

if [[ "$tag" == *-* ]]; then
  printf '\n**Pre-release.** Homebrew, apt and dnf keep the latest stable release; use the packages or archives below to try this one.\n'
fi

# The tag must already be on GitHub for this part.
echo
gh api "repos/${repo}/releases/generate-notes" -f tag_name="$tag" --jq .body
