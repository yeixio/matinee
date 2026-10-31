#!/bin/sh
# Signs Windows programs and installers with Azure Artifact Signing (formerly
# Trusted Signing), using jsign. scripts/package-windows.sh calls it when
# WINDOWS_SIGNING=1.
# Usage: scripts/sign-windows.sh file.exe...
# Needs, from the release workflow's secrets and variables:
#   AZURE_SIGNING_TOKEN     an access token for https://codesigning.azure.net
#   AZURE_SIGNING_ENDPOINT  the account's region endpoint, such as eus.codesigning.azure.net
#   AZURE_SIGNING_ACCOUNT   the signing account's name
#   AZURE_SIGNING_PROFILE   its certificate profile's name
#   JSIGN_JAR               jsign's .jar
set -eu
: "${AZURE_SIGNING_TOKEN:?}" "${AZURE_SIGNING_ENDPOINT:?}" "${AZURE_SIGNING_ACCOUNT:?}" "${AZURE_SIGNING_PROFILE:?}" "${JSIGN_JAR:?}"
for f in "$@"; do
	java -jar "$JSIGN_JAR" --storetype TRUSTEDSIGNING \
		--keystore "$AZURE_SIGNING_ENDPOINT" \
		--storepass "$AZURE_SIGNING_TOKEN" \
		--alias "$AZURE_SIGNING_ACCOUNT/$AZURE_SIGNING_PROFILE" \
		--tsaurl http://timestamp.acs.microsoft.com --tsmode RFC3161 \
		--replace "$f"
done
