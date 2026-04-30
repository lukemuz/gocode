# Build a static binary for the recipe, then ship it on a distroless base.
# Build context is the repo root: `docker build -f examples/recipes/07-web-service/Dockerfile .`

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/server ./examples/recipes/07-web-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
ENV PORT=8080 SESSIONS_DIR=:memory:
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
