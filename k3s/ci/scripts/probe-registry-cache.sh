#!/usr/bin/env sh
# Probe whether a registry cache tag exists. ACR (and most cloud registries)
# speak the Bearer token dance: /v2 answers 401 with a WWW-Authenticate realm,
# the realm exchanges basic credentials for an access token, and the manifest
# HEAD carries it. Exit 0 and print "exists" when the tag is present.
#
# Env inputs:
#   CACHE_IMAGE   full image ref host/namespace/repo:tag
#   ACR_USER      registry username
#   ACR_PASS      registry password
set -eu

host=$(printf '%s' "$CACHE_IMAGE" | cut -d/ -f1)
repo=$(printf '%s' "$CACHE_IMAGE" | cut -d/ -f2- | cut -d: -f1)
tag=$(printf '%s' "$CACHE_IMAGE" | cut -d: -f2)

auth_header=$(curl -sI "https://$host/v2/" | grep -i '^www-authenticate:' | head -1 | tr -d '\r')
realm=$(printf '%s' "$auth_header" | cut -d'"' -f2)
service=$(printf '%s' "$auth_header" | cut -d'"' -f4)

if [ -z "$realm" ] || [ -z "$service" ]; then
  echo "no bearer realm discovered; treating cache as absent" >&2
  echo "missing"
  exit 0
fi

tok=$(curl -s -u "$ACR_USER:$ACR_PASS" "$realm?service=$service&scope=repository:$repo:pull" |
  cut -d'"' -f4)

if [ -z "$tok" ]; then
  echo "token exchange failed; treating cache as absent" >&2
  echo "missing"
  exit 0
fi

code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $tok" \
  -I "https://$host/v2/$repo/manifests/$tag" || true)

if [ "$code" = "200" ]; then
  echo "exists"
else
  echo "missing (probe $code)"
fi
