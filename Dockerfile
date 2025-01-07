FROM golang:1.23.3 AS build
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o project_store

FROM alpine:latest
WORKDIR /root/
COPY --from=build /app/project_store .
EXPOSE 8080
CMD ["./project_store"]