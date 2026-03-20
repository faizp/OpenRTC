FROM golang:1.22 AS build

WORKDIR /src

COPY server/go.mod server/go.sum ./server/
COPY go.work ./
RUN cd /src/server && go mod download

COPY server ./server

RUN cd /src/server && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/openrtc-runtime ./cmd/openrtc-runtime && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/openrtc-admin ./cmd/openrtc-admin

FROM gcr.io/distroless/base-debian12

COPY --from=build /out/openrtc-runtime /usr/local/bin/openrtc-runtime
COPY --from=build /out/openrtc-admin /usr/local/bin/openrtc-admin

ENTRYPOINT ["/usr/local/bin/openrtc-runtime"]
