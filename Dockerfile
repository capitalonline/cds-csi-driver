FROM golang:1.13.6-alpine AS build-env
RUN apk update && apk add git make
COPY . /go/src/github.com/capitalonline/cds-csi-driver
RUN cd /go/src/github.com/capitalonline/cds-csi-driver && make container-binary

FROM alpine:3.7
RUN apk update --no-cache && apk add ca-certificates
COPY --from=build-env /cds-csi-driver /cds-csi-driver

ENTRYPOINT ["/cds-csi-driver"]
