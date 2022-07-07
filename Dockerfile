# Build the manager binary
FROM golang:1.15.3 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY . .

# Build
RUN CGO_ENABLED=0 GO111MODULE=on GOFLAGS=-mod=vendor go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:latest
WORKDIR /
COPY --from=builder /workspace/manager .
ENTRYPOINT ["/manager"]
