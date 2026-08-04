FROM golang:latest

WORKDIR /app

# Copy source code
COPY . .

# Enable CGO (Required for tree-sitter C bindings)
ENV CGO_ENABLED=1

# Build and explicitly output the binary to /mergetool
RUN go build -v -o /mergetool .

# Set the entrypoint to the absolute path of the binary
ENTRYPOINT ["/mergetool"]