#FROM golang:1.20 AS build-env
#COPY server.go /server.go
#RUN  GOARCH="amd64" GOOS="linux" go build -o /oss-server /server.go

FROM alpine:3.16
COPY oss-server oss.service install-oss.sh /
