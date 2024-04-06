FROM golang:1.22 as builder

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o clauganxo

FROM alpine

COPY --from=builder /app/clauganxo /usr/local/bin/clauganxo

ENTRYPOINT [ "clauganxo" ]