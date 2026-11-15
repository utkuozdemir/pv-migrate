FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS build
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
