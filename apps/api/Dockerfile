FROM golang:1.25.14-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Stamp the build into the binary so the running service can report what it is
# (issue #1117). Both default to a usable value when the args are not supplied.
ARG VERSION=dev
ARG COMMIT=""
RUN CGO_ENABLED=0 GOOS=linux go build     -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"     -o /server ./cmd/api

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
