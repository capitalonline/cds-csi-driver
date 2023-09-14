FROM golang:1.18-alpine AS build-env
COPY server.go /server.go
RUN go build -o /oss-server /server.go

FROM alpine:3.16
COPY --from=build-env /oss-server /oss-server
COPY oss.service /oss.service