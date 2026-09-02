# syntax=docker/dockerfile:1.26
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
# GO_VERSION has no default on purpose: the Taskfile passes the version go.mod
# declares, so the toolchain cannot drift from the module.

ARG GO_VERSION
# renovate: depName=golangci/golangci-lint datasource=github-releases
ARG GOLANGCI_LINT_VERSION=2.13.2
# renovate: depName=mvdan/sh datasource=github-releases
ARG SHFMT_VERSION=3.14.0
# the single declaration of the goreleaser version: the workflows read it from
# here rather than carrying their own, so the version that validates the release
# config is always the version that performs the release
# renovate: depName=goreleaser/goreleaser datasource=docker
ARG GORELEASER_VERSION=v2.18.0
# renovate: depName=alpine/helm datasource=docker
ARG HELM_VERSION=4.2.4
# renovate: depName=jnorwood/helm-docs datasource=docker
ARG HELM_DOCS_VERSION=v1.14.2

FROM goreleaser/goreleaser:${GORELEASER_VERSION} AS goreleaser-bin
FROM alpine/helm:${HELM_VERSION} AS helm-bin
FROM jnorwood/helm-docs:${HELM_DOCS_VERSION} AS helm-docs-bin

# released binaries, fetched rather than compiled, so no check waits on a
# toolchain build. The downloads are a linear chain: bumping an earlier tool
# re-fetches the later ones, which is a few seconds and not worth a stage per
# tool.
FROM alpine:3@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS tools
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
FROM golang:${GO_VERSION} AS deps
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
