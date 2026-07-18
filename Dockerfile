FROM golang:latest
WORKDIR /tree-ast-diff
COPY . .
# The Debian-based Go image has a working GCC pre-installed
RUN go build -o mergetool .
ENTRYPOINT ["/tree-ast-diff/mergetool"]