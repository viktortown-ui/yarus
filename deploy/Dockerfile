# Public deployments must be reviewed and built using a maintained Go version.
# This recipe is supplied but has NOT been built/deployed in this environment.
FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /yarus .
FROM debian:bookworm-slim
RUN groupadd --gid 10001 yarus && useradd --uid 10001 --gid 10001 --create-home yarus && mkdir /data && chown yarus:yarus /data
COPY --from=build /yarus /usr/local/bin/yarus
USER 10001:10001
VOLUME /data
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/yarus", "--headless", "--data", "/data"]
