FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w" -trimpath -o bin/gatus-sidecar cmd/root.go

FROM scratch
COPY --from=builder /src/bin/gatus-sidecar /gatus-sidecar
ENTRYPOINT ["/gatus-sidecar"]
