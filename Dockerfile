FROM golang:1.22-alpine3.19 AS builder
WORKDIR /src

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=none

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.buildVersion=${BUILD_VERSION} -X main.buildCommit=${BUILD_COMMIT}" \
    -o /out/app ./cmd/app

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/app /app/app
USER 65532:65532
EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=2s --start-period=3s --retries=3 \
    CMD ["/app/app", "healthcheck"]

ENTRYPOINT ["/app/app"]
CMD ["server"]

