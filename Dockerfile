# The builder always runs on the machine doing the building, never emulated:
# BUILDPLATFORM is the host's own platform, and Go cross-compiles to the target
# by setting GOARCH. Letting the builder stage run as the *target* platform
# instead would drag every compile through QEMU for no benefit.
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

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
    go build -ldflags="-s -w -X shopify-monitor/internal/config.Build=${BUILD}" -o app .

# ─────────────────────────────────────────────────────────────

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/app .
COPY --from=builder /app/config.yaml .


CMD [ "./app" ]
