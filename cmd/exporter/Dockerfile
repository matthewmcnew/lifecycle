ARG go_version=1.10.1

FROM golang:${go_version} as deps

ARG lifecycle_repo=github.com/buildpack/lifecycle

COPY . "/go/src/${lifecycle_repo}"
RUN CGO_ENABLED=0 go install -a -installsuffix static "${lifecycle_repo}/cmd/exporter"

FROM packs/v3

USER root

COPY --from=deps /go/bin /packs

USER packs

ENV PACK_METADATA_PATH ./metadata.toml

ENTRYPOINT ["/packs/exporter"]
