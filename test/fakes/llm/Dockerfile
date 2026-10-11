# Fake language-model server (spec/ai/mocks.md sec. 5). Build from the repo root:
#   docker build -f test/fakes/llm/Dockerfile -t counsel/llm-fake .
# Serves the fixtures baked in from testdata/ai; mount another dir at /fixtures to override.
# It makes no outbound calls; run it on an internal network.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY kernel/ai ./kernel/ai
COPY test/fakes/llm ./test/fakes/llm
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/llm-fake ./test/fakes/llm

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/llm-fake /llm-fake
COPY testdata/ai /fixtures
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --retries=5 CMD ["/llm-fake", "-healthcheck", "-addr", ":8080"]
ENTRYPOINT ["/llm-fake", "-addr", ":8080", "-fixtures", "/fixtures"]
