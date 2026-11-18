FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/nccl-fabric-nri ./cmd/nccl-fabric-nri

FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source=https://github.com/nscaledev/nvidia-platform-chart
COPY --from=build /out/nccl-fabric-nri /nccl-fabric-nri
USER 65532:65532
ENTRYPOINT ["/nccl-fabric-nri"]
