# ---- build stage ----
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Generate templ code, then build a static binary.
RUN go tool templ generate
RUN CGO_ENABLED=0 go build -o /out/linker ./cmd/linker

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/linker /app/linker
EXPOSE 8080
ENTRYPOINT ["/app/linker"]
