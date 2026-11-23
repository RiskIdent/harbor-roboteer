#!/usr/bin/env bash

set -euo pipefail

VERSION="${1:?missing required arg}"
# Example valid GORELEASER_ARTIFACTS value (since actual artifacts.json is massive this is small example of the relevant parts):
# [
#   {
#     "name": "goreleaser.ko.local/harbor-roboteer:5c9b92bc5e1bf09362d55ac6b075f5919cabfb28eb5a1d22814ff55971161393",
#     "extra": {
#       "Digest": "5c9b92bc5e1bf09362d55ac6b075f5919cabfb28eb5a1d22814ff55971161393",
#       "ID": "harbor-roboteer"
#     }
#   }
# ]
GORELEASER_ARTIFACTS="${GORELEASER_ARTIFACTS:-}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "::error::Version must follow pattern 'v1.2.3', but got: '$VERSION'" >&2
fi

chart=helm/harbor-roboteer/Chart.yaml
name=harbor-roboteer
app_version="$VERSION"

if [[ -n "$GORELEASER_ARTIFACTS" ]]; then
    digest="$(jq -r --arg name "$name" 'first(.[] | select(.type == "Docker Manifest" and .extra.ID == $name) | .extra.Digest) // empty' <<<"$GORELEASER_ARTIFACTS")"
    if [[ -z "$digest" ]]; then
        echo "::error::No image digest found for chart '$name'" >&2
        exit 1
    fi
    app_version="$VERSION@$digest"
fi

sed -i "s|^version:.*|version: $VERSION|; s|^appVersion:.*|appVersion: \"$app_version\"|" "$chart"

echo "Updated $chart:"
echo "  version: $VERSION"
echo "  appVersion: $app_version"
