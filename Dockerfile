# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/smtp2paperless ./cmd/smtp2paperless

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/smtp2paperless /smtp2paperless

# Unprivileged port; map it to 25 on the host if the scanner cannot use another one.
EXPOSE 2525 9090
ENV SMTP_PORT=2525 \
    METRICS_PORT=9090

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/smtp2paperless", "healthcheck"]
ENTRYPOINT ["/smtp2paperless"]
