#!/usr/bin/env bash
set -euo pipefail

IMAGE=myorg/myapp ## Change to your own

sudo rm -rf /tmp/packsv3/{platform,launch}
mkdir -p /tmp/packsv3/{platform,cache,launch}
cp -r ~/workspace/nodejs-buildpack/fixtures/node_version_range /tmp/packsv3/launch/app
# rm /tmp/packsv3/launch/app/node_modules

docker run -w /launch/app -v /tmp/packsv3/launch/app:/launch/app packs/v3:detect
touch /tmp/packsv3/launch/app/detect.toml

# ## Temp
# ./v3/bin/build packs/cflinuxfs2
# docker run -it -w /launch/app -v /tmp/packsv3/cache:/cache -v /tmp/packsv3/launch:/launch -v /tmp/packsv3/launch/app:/launch/app --entrypoint bash packs/v3:build
# exit 1

docker run -w /launch/app \
  -v /tmp/packsv3/platform:/platform \ ## Not used, here for future input. TODO: Should it be?
  -v /tmp/packsv3/cache:/cache \
  -v /tmp/packsv3/launch:/launch \
  -v /tmp/packsv3/launch/app:/launch/app \
  packs/v3:build

docker run -w /launch/app \
  --user 0 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/packsv3/launch:/launch \
  -v /tmp/packsv3/launch/app:/launch/app \
  packs/v3:export \
  -daemon \
  -stack packs/v3:run \
  "$IMAGE"

docker run -e PORT=8080 -p 8080:8080 "$IMAGE"
