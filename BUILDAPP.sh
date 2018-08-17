#!/usr/bin/env bash
set -euo pipefail
set -x

cd $(dirname "${BASH_SOURCE[0]}")

rm -rf /tmp/packsv3/{bin,platform,launch,workspace,docker_config}
mkdir -p /tmp/packsv3/{bin,platform,cache,launch,workspace,docker_config}
echo '{}' > /tmp/packsv3/docker_config/config.json
# cp -r ~/workspace/nodejs-buildpack/fixtures/node_version_range /tmp/packsv3/launch/app
cp -r ../pack/acceptance/fixtures/node_app /tmp/packsv3/launch/app

# IMAGE=myorg/myapp ## Change to your own
# STACK=packs/v3
IMAGE=host-machine.local:32806/myapp
STACK=host-machine.local:32806/packs/v3

cat >/tmp/packsv3/Dockerfile <<EOL
FROM golang:1.10 as lifecycle

RUN go get github.com/sclevine/yj
RUN wget -qO /go/bin/jq http://stedolan.github.io/jq/download/linux64/jq && chmod +x /go/bin/jq

COPY . /go/src/github.com/buildpack/lifecycle
RUN CGO_ENABLED=0 go install -a -installsuffix static "github.com/buildpack/lifecycle/cmd/..."

FROM ubuntu:18.04 as buildpacks

RUN apt-get update && apt-get install -y curl
RUN mkdir -p /buildpacks/sh.packs.samples.buildpack.nodejs/latest
RUN curl -sSL https://api.github.com/repos/buildpack/samples/tarball/master | tar -xzf - -C /buildpacks/sh.packs.samples.buildpack.nodejs/latest --strip-components=2 --wildcards 'buildpack-samples-*/nodejs-buildpack'
RUN echo 'groups = [{ repository = "packs/v3", buildpacks = [{ id = "sh.packs.samples.buildpack.nodejs", version = "latest" }] }]' > /buildpacks/order.toml

FROM ubuntu:18.04

ENV PACK_BP_GROUP_PATH ./group.toml
ENV PACK_BP_ORDER_PATH /buildpacks/order.toml
ENV PACK_BP_PATH /buildpacks
ENV PACK_DETECT_INFO_PATH ./detect.toml
ENV PACK_METADATA_PATH ./metadata.toml
ENV PACK_METADATA_PATH /launch/config/metadata.toml
ENV PACK_PROCESS_TYPE web
ENV PACK_STACK_NAME packs/v3

RUN useradd -u 1000 -mU -s /bin/bash packs
COPY --from=lifecycle /go/bin /packs
COPY --from=buildpacks /buildpacks /buildpacks
RUN ln -s /packs/yj /bin/yj && ln -s /packs/jq /bin/jq

WORKDIR /workspace
RUN chown -R packs /workspace
USER packs
EOL

docker build -t lifecycle_dev -f - . < /tmp/packsv3/Dockerfile

docker run \
  -v /tmp/packsv3/launch/app:/launch/app \
  -v /tmp/packsv3/workspace:/workspace \
  lifecycle_dev \
  /packs/detector

time docker run \
  --user 0 \
  --add-host 'host-machine.local:172.17.0.1' \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/packsv3/docker_config/config.json:/root/.docker/config.json \
  -v /tmp/packsv3/launch:/launch \
  -v /tmp/packsv3/workspace:/workspace:ro \
  lifecycle_dev \
  /packs/analyzer \
  "$IMAGE"
time docker run \
  --user 0 \
  -v /tmp/packsv3/launch:/launch \
  lifecycle_dev \
  chown -R packs:packs /launch

time docker run \
  -v /tmp/packsv3/platform:/platform \
  -v /tmp/packsv3/cache:/cache \
  -v /tmp/packsv3/launch:/launch \
  -v /tmp/packsv3/workspace:/workspace \
  lifecycle_dev \
  /packs/builder

time docker run \
  --user 0 \
  --add-host 'host-machine.local:172.17.0.1' \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/packsv3/docker_config/config.json:/root/.docker/config.json \
  -v /tmp/packsv3/launch:/launch:ro \
  -v /tmp/packsv3/workspace:/workspace:ro \
  lifecycle_dev \
  /packs/exporter \
  -stack $STACK \
  "$IMAGE"


LOCAL_IMAGE_NAME="$(echo "$IMAGE" | sed 's/host-machine.local/localhost/')"
docker pull "$LOCAL_IMAGE_NAME"
docker inspect -f '{{index .Config.Labels "sh.packs.build"}}' "$LOCAL_IMAGE_NAME" | jq .

label="test_packs_app_$RANDOM"
docker run "--name=$label" --rm=true -d -e PORT=8080 -p 8080:8080 "$LOCAL_IMAGE_NAME"
sleep 1
curl -i http://localhost:8080 || (sleep 1 && curl -i http://localhost:8080)
echo
docker kill "$label"
