FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/posixmeta ./cmd/posixmeta
COPY internal/posixmeta ./internal/posixmeta
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GOARM="${TARGETVARIANT#v}" \
    go build -trimpath -ldflags="-s -w" -o /out/posixmeta ./cmd/posixmeta

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

COPY --from=build /out/posixmeta /usr/local/bin/posixmeta

# Keep this UID/GID in sync with nonRootUID in internal/bucketstorage.
RUN apk add --no-cache rclone && \
    addgroup -g 10000 pvmigrate && \
    adduser -D -u 10000 -G pvmigrate -h /home/pvmigrate pvmigrate && \
    chown -R pvmigrate:pvmigrate /home/pvmigrate
