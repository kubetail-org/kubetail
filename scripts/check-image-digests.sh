#!/bin/sh
# Every image a Dockerfile pulls is pinned by digest, its tag kept for reading
# (alpine:3.24.4@sha256:…), so a re-pushed tag cannot change a release.
# Prints every FROM or COPY --from that names an image with no digest, and
# fails. scratch and earlier stages are not images.
#
#   sh scripts/check-image-digests.sh
set -eu
cd "$(dirname "$0")/.."

status=0
for file in build/package/Dockerfile.* hack/tilt/Dockerfile.*; do
  bad="$(awk '
    function check(image) {
      if (image == "scratch" || (tolower(image) in stages) || image ~ /^[0-9]+$/) return
      if (image !~ /@sha256:[0-9a-f]+$/) print FILENAME ":" FNR ":" $0
    }
    toupper($1) == "FROM" {
      image = ""
      for (i = 2; i <= NF; i++) {
        if ($i ~ /^--/) continue
        if (image == "") { image = $i; continue }
        if (toupper($i) == "AS" && i < NF) stage = tolower($(i + 1))
      }
      check(image)
      if (stage != "") stages[stage] = 1
      stage = ""
    }
    toupper($1) == "COPY" {
      for (i = 2; i <= NF; i++)
        if ($i ~ /^--from=/) check(substr($i, 8))
    }
  ' "$file")"
  if [ -n "$bad" ]; then
    printf '%s\n' "$bad"
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "pin each image by digest: <image>:<tag>@sha256:<digest>" >&2
fi
exit "$status"
