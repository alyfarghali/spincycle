<<<<<<< HEAD
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/ingest ./services/ingest
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ingest ./services/ingest

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /ingest /ingest
ENTRYPOINT ["/ingest"]
=======
# Container image for the ingest service.
>>>>>>> main
