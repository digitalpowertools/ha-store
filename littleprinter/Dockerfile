ARG BUILD_FROM=ghcr.io/home-assistant/armv7-base-debian:bookworm

# --- build stage -----------------------------------------------------------
FROM golang:1.27-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags "-s -w" -o /out/littleprinter .

# --- runtime stage ---------------------------------------------------------
FROM ${BUILD_FROM}

# HA add-on labels (used by the supervisor for metadata)
LABEL \
    io.hass.name="Little Printer" \
    io.hass.description="BERG Little Printer server (Go sirius replacement)" \
    io.hass.arch="${BUILD_ARCH}" \
    io.hass.type="addon" \
    io.hass.version="${BUILD_VERSION}"

COPY --from=builder /out/littleprinter /usr/local/bin/littleprinter
COPY run.sh /run.sh
RUN chmod +x /run.sh

ENTRYPOINT ["/run.sh"]
