FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /autoscaler .

FROM alpine:3.20
RUN apk add --no-cache docker-cli
COPY --from=build /autoscaler /autoscaler
ENTRYPOINT ["/autoscaler"]