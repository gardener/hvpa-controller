# Build the manager binary
FROM golang:1.21.8 as builder

WORKDIR /go/src/github.com/gardener/hvpa-controller
# Copy the Go Modules manifests
COPY . .

# Build
RUN CGO_ENABLED=0 GO111MODULE=on GOFLAGS=-mod=vendor go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static-debian11:nonroot
WORKDIR /

COPY --from=builder /go/src/github.com/gardener/hvpa-controller/manager .
ENTRYPOINT ["/manager"]
