FROM golang:1.23-bookworm

WORKDIR /app

# Install the native build toolchain required by Tree-sitter's C bindings.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy source code
COPY . .

# Enable CGO so the Tree-sitter native libraries can be built.
ENV CGO_ENABLED=1

# Build and explicitly output the binary to /mergetool
RUN go build -v -o /mergetool .

# Health check: confirm the CLI still responds
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/mergetool", "--help"] >/dev/null 2>&1 || exit 1

# Set the entrypoint to the absolute path of the binary
ENTRYPOINT ["/mergetool"]