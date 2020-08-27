FROM golang:1.13.6-alpine AS build-env
RUN apk update && apk add git make
COPY . /go/src/github.com/capitalonline/cds-csi-driver
RUN cd /go/src/github.com/capitalonline/cds-csi-driver && make container-binary

FROM alpine:3.6
RUN apk update --no-cache && apk add ca-certificates
ARG S3FS_VERSION=v1.82

RUN apk --update add --virtual build-dependencies \
        build-base alpine-sdk \
        fuse fuse-dev \
        automake autoconf git \
        libressl-dev  \
        curl-dev libxml2-dev  \
        ca-certificates

# RUN apk del .build-dependencies
RUN git clone https://github.com/s3fs-fuse/s3fs-fuse.git && \
    cd s3fs-fuse \
    git checkout tags/${S3FS_VERSION} && \
    ./autogen.sh && \
    ./configure --prefix=/usr && \
    make && \
    make install

RUN s3fs --version

COPY --from=build-env /cds-csi-driver /cds-csi-driver
COPY /lib/udev /lib/udev

ENTRYPOINT ["/cds-csi-driver"]
