# Official MinIO source pinned to RELEASE.2025-04-22T22-12-26Z.
# Published MinIO images are no longer available to clean CI runners.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
RUN git init && git remote add origin https://github.com/minio/minio.git && \
    git fetch --depth=1 origin 0d7408fc9969caf07de6a8c3a84f9fbb10a6739e && git checkout FETCH_HEAD
RUN CGO_ENABLED=0 go build -trimpath -o /minio .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /minio /usr/local/bin/minio
ENTRYPOINT ["minio"]
