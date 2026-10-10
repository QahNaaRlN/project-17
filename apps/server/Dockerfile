# Образ сервера CMS. Контекст сборки — apps/server.
FROM mirror.gcr.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/qahnaarln/project-17/apps/server/internal/app.Version=${VERSION}" \
      -o /out/cms ./cmd/cms

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cms /cms
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/cms"]
CMD ["serve"]
