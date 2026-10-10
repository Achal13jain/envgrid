# syntax=docker/dockerfile:1

# Build stages run on the build machine's platform and cross-compile, so a
# multi-arch image needs no emulation.

FROM --platform=$BUILDPLATFORM node:25-alpine@sha256:bdf2cca6fe3dabd014ea60163eca3f0f7015fbd5c7ee1b0e9ccb4ced6eb02ef4 AS ui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:738d1cf061836894ff6bb8c33881080ac66de8cf0586615012a0c8f592649cfa AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Every Go file of the main package; *_test.go files are not compiled.
COPY *.go ./
COPY internal ./internal
COPY --from=ui /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/envgrid . \
 && mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/envgrid /envgrid
# Licences of envgrid and of the code and fonts built into it.
COPY LICENSE THIRD_PARTY_NOTICES.md /licenses/
COPY licenses /licenses/
# The volume mount point, owned by the nonroot user (65532) so a fresh named
# volume is writable.
COPY --from=build --chown=65532:65532 /out/data /data
ENV ENVGRID_DATA_DIR=/data \
    ENVGRID_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/envgrid"]
CMD ["serve"]
