# Cross-compiled with Go rather than emulated, so a multi-arch build needs no
# QEMU: the build stage always runs on the builder's native platform and
# GOOS/GOARCH come from the target.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /fftui .

# The cache dir is created here because the runtime image has no shell to mkdir
# with. Docker seeds a fresh named volume from the image's directory, so this is
# what makes /data writable by the nonroot user.
RUN mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /fftui /fftui
COPY --from=build --chown=65532:65532 /data /data

# The token cache lives under os.UserCacheDir(), which reads XDG_CACHE_HOME
# first — point it at the volume so a minted token survives a restart.
ENV XDG_CACHE_HOME=/data
# Loopback is the right default for a bare binary and the wrong one inside a
# container: nothing outside the network namespace could reach it.
ENV FF_WEB_ADDR=0.0.0.0:8442

EXPOSE 8442
ENTRYPOINT ["/fftui", "--web", "--headless"]
