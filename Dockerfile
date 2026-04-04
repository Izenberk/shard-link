# Stage 1: Build the Go Hub
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build for our "Remembrance" daemon
RUN CGO_ENABLED=0 GOOS=linux go build -o /shard-link ./main.go

# Stage 2: Final Lean Image
FROM gcr.io/distroless/static
WORKDIR /
COPY --from=builder /shard-link /shard-link
EXPOSE 8080
ENTRYPOINT [ "/shard-link" ]