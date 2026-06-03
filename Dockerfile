FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/brain-mcp ./cmd/brain-mcp

FROM alpine:3.22

RUN apk add --no-cache ca-certificates git

COPY --from=build /out/brain-mcp /usr/local/bin/brain-mcp

EXPOSE 8787

ENTRYPOINT ["brain-mcp"]
