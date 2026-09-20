# syntax=docker/dockerfile:1.27
#
# The development and CI toolchain, one stage per check.
#
# Every tool this repository lints, formats or tests with lives here, pinned, so
# a fresh clone needs only docker and task on the host and CI runs the exact
# same versions instead of installing its own. The Taskfile drives the stages
# through `docker buildx build --target`.
#
# What is deliberately NOT here: anything that needs a cluster or a terminal.
# The integration suites, the demo recordings and the release tagging keep their
# own tools on the host, because a sealed image cannot give them a kube context,
# a GPU-less framebuffer or a push credential.
#
# Cache shape matters and is easy to lose. Tools arrive as released binaries
# rather than compiled from source, so no check waits on a toolchain build, and
# the module graph is downloaded in a stage that does not sit downstream of the
# tools, so bumping a linter does not re-download it.
#
# renovate: depName=golangci/golangci-lint datasource=github-releases
ARG GOLANGCI_LINT_VERSION=2.13.2
# renovate: depName=mvdan/sh datasource=github-releases
ARG SHFMT_VERSION=3.14.1
# the single declaration of the goreleaser version: the workflows read it from
# here rather than carrying their own, so the version that validates the release
# config is always the version that performs the release
ARG GORELEASER_VERSION=v2.18.2

FROM goreleaser/goreleaser:${GORELEASER_VERSION}@sha256:7077423cf5ef643ff56a34b58f93c1364e927e5c3dfa470eeabc44cab1a9c72b AS goreleaser-bin
FROM alpine/helm:4.3.0@sha256:a6cf54599ccb99d90cf0712b30f03fdb3cab062e6b94e0418cc4db7e8a1464b2 AS helm-bin
FROM jnorwood/helm-docs:v1.14.2@sha256:7e562b49ab6b1dbc50c3da8f2dd6ffa8a5c6bba327b1c6335cc15ce29267979c AS helm-docs-bin

# released binaries, fetched rather than compiled, so no check waits on a
# toolchain build. The downloads are a linear chain: bumping an earlier tool
# re-fetches the later ones, which is a few seconds and not worth a stage per
# tool.
FROM alpine:3@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS tools
RUN apk add --no-cache curl tar git
ARG TARGETARCH
ARG GOLANGCI_LINT_VERSION
RUN curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin --strip-components=1 "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}/golangci-lint"
ARG SHFMT_VERSION
RUN curl -sSfL -o /usr/local/bin/shfmt "https://github.com/mvdan/sh/releases/download/v${SHFMT_VERSION}/shfmt_v${SHFMT_VERSION}_linux_${TARGETARCH}" \
    && chmod +x /usr/local/bin/shfmt
COPY --from=goreleaser-bin /usr/bin/goreleaser /usr/local/bin/goreleaser
COPY --from=helm-bin /usr/bin/helm /usr/local/bin/helm
COPY --from=helm-docs-bin /usr/bin/helm-docs /usr/local/bin/helm-docs

# the Go toolchain and the module graph. Deliberately NOT downstream of `tools`:
# a linter bump must not re-download the modules.
# The image ships GOTOOLCHAIN=local, so a go.mod that declares a newer Go fails
# the download here instead of fetching a toolchain behind the pinned digest.
FROM golang:1.27.1@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build,id=pv_migrate/go-build \
    --mount=type=cache,target=/go/pkg,id=pv_migrate/go-pkg \
    go mod download

# the Go tree plus the embedded chart, which the Go tests render. A docs or
# packaging edit must not rerun the Go checks.
FROM deps AS base
COPY cmd ./cmd
COPY internal ./internal
COPY pvmigrate ./pvmigrate
COPY integration ./integration
COPY hack ./hack

FROM base AS lint-golangci-lint
COPY .golangci.yml ./
COPY --from=tools /usr/local/bin/golangci-lint /usr/local/bin/
RUN --mount=type=cache,target=/root/.cache/go-build,id=pv_migrate/go-build \
    --mount=type=cache,target=/go/pkg,id=pv_migrate/go-pkg \
    --mount=type=cache,target=/root/.cache/golangci-lint,id=pv_migrate/golangci-lint \
    golangci-lint run --timeout=10m ./...

FROM base AS lint-go-mod-tidy
RUN --mount=type=cache,target=/root/.cache/go-build,id=pv_migrate/go-build \
    --mount=type=cache,target=/go/pkg,id=pv_migrate/go-pkg \
    go mod tidy --diff

# only the chart, so unrelated edits leave this cached
FROM tools AS lint-chart
WORKDIR /src
COPY internal/helm/pv-migrate ./internal/helm/pv-migrate
RUN helm lint internal/helm/pv-migrate

# goreleaser refuses to run outside a repository with a remote, but it only
# reads the config. A synthetic repository satisfies it, which keeps .git out of
# the build context entirely: .git changes on every commit and would otherwise
# invalidate every stage that copies the tree.
FROM tools AS lint-release
WORKDIR /src
COPY .goreleaser.yml ./
RUN git init -q . \
    && git remote add origin https://github.com/utkuozdemir/pv-migrate.git \
    && goreleaser check

# the formatters, the generators and the shell checks run against a bind-mounted
# working tree rather than a stage export. Shell is not a stage of its own on
# purpose: the file list comes from git on the host, so a scratch directory or a
# second worktree checked out under this one cannot reach the check.
FROM deps AS fmt
COPY --from=tools /usr/local/bin/golangci-lint /usr/local/bin/shfmt \
    /usr/local/bin/goreleaser /usr/local/bin/helm-docs /usr/local/bin/
