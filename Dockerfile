FROM golang:1.19-alpine AS build

WORKDIR /build
COPY . .
RUN go build

FROM alpine

COPY --from=build /build/airthings_exporter /airthings_exporter
ENTRYPOINT ["/airthings_exporter"]
