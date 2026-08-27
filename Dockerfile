# The builder always runs on the machine doing the building, never emulated:
# BUILDPLATFORM is the host's own platform, and Go cross-compiles to the target
# by setting GOARCH. Letting the builder stage run as the *target* platform
# instead would drag every compile through QEMU for no benefit.
# Pinned rather than floating on :alpine so a rebuild cannot silently change
# toolchains, and so Renovate has a version to raise a PR against. This is the
# current stable Go, not go.mod's `go 1.23.0` — that directive is the minimum
# language version the source needs, not the compiler that has to build it.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

# Supplied by buildx, one value per platform being built.
ARG TARGETOS
ARG TARGETARCH

# Identifies the running version. Without it every deployed image reports
# "develop", so a container cannot tell you which commit it is — which is
# exactly the question worth asking when a deploy looks like it did not land.
ARG BUILD=develop

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X github.com/chewycrunch/shopify-monitor/internal/config.Build=${BUILD}" -o app .

# ─────────────────────────────────────────────────────────────

FROM alpine:3.24

# What links the published package to this repo on GHCR and makes the package
# page's "Source" link resolve. metadata-action sets the same label in CI; this
# covers images built by hand from a laptop.
LABEL org.opencontainers.image.source="https://github.com/chewycrunch/shopify-monitor"

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/app .
# COPY --from=builder /app/config.yaml .


CMD [ "./app" ]
