FROM golang:1.20 AS build-env
COPY server.go /server.go
RUN  GOARCH="amd64" GOOS="linux" go build -o /oss-server /server.go
#RUN  GOARCH="amd64" GOOS="linux" go build -ldflags '-linkmode=external' -o /oss-server /server.go

FROM alpine:3.16
COPY --from=build-env /oss-server /oss-server
COPY oss.service /oss.service
COPY install-oss.sh  /install-oss.sh 
