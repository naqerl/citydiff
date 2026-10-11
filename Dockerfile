FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=1
# VERSION names a release build. Without it the build is a development build,
# and COMMIT names it, since .git is not copied in.
ARG VERSION=
ARG COMMIT=
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/citydiff ./cmd/cli

FROM debian:bookworm-slim

COPY --from=build /out/citydiff /usr/local/bin/citydiff

EXPOSE 8787
WORKDIR /work
ENTRYPOINT ["citydiff"]
CMD ["-path", "/work", "-addr", "0.0.0.0:8787"]
