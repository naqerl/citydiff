FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=1
RUN go build -trimpath -ldflags "-s -w" -o /out/citydiff ./cmd/cli

FROM debian:bookworm-slim

COPY --from=build /out/citydiff /usr/local/bin/citydiff

EXPOSE 8787
WORKDIR /work
ENTRYPOINT ["citydiff"]
CMD ["-path", "/work", "-view", "-addr", "0.0.0.0:8787"]
