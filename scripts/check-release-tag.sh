#!/usr/bin/env bash
# A release tag is a component and a SemVer version: `dashboard/v1.2.3`,
# `cli/v1.2.3-rc.1`. release-docker.yml publishes and signs what it is told to
# build, so it builds nothing else. Fails on any other name.
#
#   bash scripts/check-release-tag.sh <tag>
set -eu

tag="${1:-}"
release_tag='^(dashboard|cluster-api|cluster-agent|cli)/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

if [[ ! "$tag" =~ $release_tag ]]; then
  echo "not a release tag: $tag" >&2
  exit 1
fi
