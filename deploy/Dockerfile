FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /openai-proxy ./cmd/openai-proxy

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget tzdata su-exec \
    && adduser -D -u 1000 proxy
WORKDIR /app
COPY --from=build /openai-proxy /app/openai-proxy
COPY LICENSE /app/LICENSE
COPY deploy/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh
LABEL org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.description="Unofficial OpenAI-compatible proxy for ChatGPT Codex (not affiliated with OpenAI)" \
      org.opencontainers.image.source="https://github.com/burzilov/openai-proxy"
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["serve"]
