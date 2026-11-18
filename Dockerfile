FROM node:24-alpine AS admin
WORKDIR /src
COPY scripts/vendor-admin.sh scripts/vendor-admin.sh
RUN ./scripts/vendor-admin.sh

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY admin/ admin/
COPY --from=admin /src/admin/vendor/ admin/vendor/
RUN mkdir -p /out/data && chmod 0700 /out/data && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/openpage .

FROM scratch
COPY --from=build /out/ /
ENV OPENPAGE_DATA_DIR=/data
EXPOSE 3000
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/openpage", "-healthcheck"]
ENTRYPOINT ["/openpage"]
