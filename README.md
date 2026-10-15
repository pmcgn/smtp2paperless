# smtp2paperless

Receives scanner emails over plain SMTP and uploads every whitelisted attachment
(default: `tiff`, `pdf`, `jpg`) to the [paperless-ngx](https://docs.paperless-ngx.com/api/#file-uploads)
`POST /api/documents/post_document/` endpoint.

No TLS and no authentication: run it only on a trusted network segment.

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `PAPERLESS_URL` | – (required) | Base URL of paperless-ngx, e.g. `http://paperless:8000` |
| `PAPERLESS_TOKEN` | – (required) | API token (`Authorization: Token …`) |
| `PAPERLESS_TOKEN_FILE` | – | Read the token from a file (Docker secrets); used if `PAPERLESS_TOKEN` is empty |
| `PAPERLESS_TAGS` | – | Comma separated tag IDs added to each document |
| `PAPERLESS_CORRESPONDENT` | – | Correspondent ID |
| `PAPERLESS_DOCUMENT_TYPE` | – | Document type ID |
| `PAPERLESS_CUSTOM_PATH` | `/api/documents/post_document/` | HTTP path (appended to `PAPERLESS_URL`) the document is POSTed to, for non-paperless-ngx targets. The leading `/` is recommended but not required – one is added if missing. Include any query string as part of the value |
| `PAPERLESS_HTTP_TIMEOUT` | `60s` | Per-request timeout |
| `PAPERLESS_UPLOAD_RETRIES` | `3` | Extra attempts on network errors / 5xx / 429 |
| `ATTACHMENT_WHITELIST` | `tiff,pdf,jpg` | Extensions to upload (`jpeg`≡`jpg`, `tif`≡`tiff`) |
| `SMTP_PORT` | `2525` | SMTP listen port |
| `SMTP_DOMAIN` | `localhost` | Name announced in the SMTP greeting |
| `SMTP_MAX_MESSAGE_BYTES` | `52428800` (50 MiB) | Maximum message size; larger mails get `552`. `0` = no limit (see below) |
| `SMTP_MAX_RECIPIENTS` | `50` | Maximum recipients per message |
| `SMTP_TIMEOUT` | `5m` | Read/write timeout |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `METRICS_PORT` | `9090` | Serves `/metrics` (Prometheus) and `/healthz` |
| `ENV_FILE` | `.env` | Optional dotenv file for local development |

Message size: SMTP has no inherent limit, so the cap is this service's own choice. When
`SMTP_MAX_MESSAGE_BYTES` is unset the 50 MiB default applies; only an explicit `0` disables the
limit. Messages are held in memory while being processed (base64 text, decoded attachment and
upload body), so plan for roughly 3-4x the message size in RAM per concurrent mail. Attachments
are ~33% larger on the wire than on disk because of base64. A reverse proxy in front of
paperless-ngx may impose its own body limit (e.g. nginx `client_max_body_size`).

Behaviour: all recipients are accepted; the message is acknowledged once every whitelisted
attachment is uploaded. If an upload fails, the sender gets `451` (temporary failure);
paperless-ngx drops duplicates by checksum, so a retry won't create double documents.

## Metrics

`smtp2paperless_messages_total{result}`, `smtp2paperless_attachments_total{result=uploaded|skipped|failed}`,
`smtp2paperless_uploaded_bytes_total`, `smtp2paperless_upload_duration_seconds`,
`smtp2paperless_paperless_requests_total{code}`, `smtp2paperless_smtp_connections_total`,
`smtp2paperless_smtp_active_connections`, plus Go runtime/process metrics.

## Run with Docker

```sh
docker build -t smtp2paperless .
docker run -d -p 25:2525 -p 9090:9090 \
  -e PAPERLESS_URL=http://paperless:8000 -e PAPERLESS_TOKEN=... smtp2paperless
```

See `docker-compose.yml`. Configure the Brother scanner's SMTP server to the host's IP, port 25,
no authentication, no encryption.

## Local development

```sh
cp .env.example .env            # edit PAPERLESS_URL / PAPERLESS_TOKEN
go run ./cmd/mockpaperless -token change-me   # optional fake paperless-ngx on :8000
go run ./cmd/smtp2paperless                   # loads .env automatically
go run ./cmd/sendtest some.pdf photo.jpg      # sends a test mail to localhost:2525
curl localhost:9090/metrics
go test ./...
```

Or all in Docker: `docker compose -f docker-compose.dev.yml up --build`.

Against a real, preconfigured paperless-ngx (UI on http://localhost:8000, login `admin`/`admin`,
API token provisioned automatically): `docker compose -f docker-compose.paperless.yml up --build`.
