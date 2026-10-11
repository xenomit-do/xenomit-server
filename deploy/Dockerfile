FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.work ./
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY services ./services
COPY spec ./spec
RUN CGO_ENABLED=0 go build -o /xenomit ./cmd/xenomit

FROM gcr.io/distroless/static-debian12
COPY --from=build /xenomit /xenomit
ENV CLIENT_ROOT=/client \
    GAME_ADDR=:8080 \
    CHAT_ADDR=:9338 \
    POLICY_ADDR=:843 \
    HTTP_ADDR=:8088
EXPOSE 8080 9338 843 8088
ENTRYPOINT ["/xenomit"]
