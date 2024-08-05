FROM golang:1.18-alpine AS build-env
RUN apk update && apk add git make
#COPY cck-sdk-go /go/src/github.com/capitalonline/cck-sdk-go
#COPY cds-csi-driver /go/src/github.com/capitalonline/cds-csi-driver

COPY . /go/src/github.com/capitalonline/cds-csi-driver
RUN go env -w GO111MODULE=on && go env -w GOPROXY=https://goproxy.cn,direct
RUN cd /go/src/github.com/capitalonline/cds-csi-driver && go mod tidy && make container-binary


FROM alpine:3.6

#ARG S3FS_VERSION=v1.82
#
RUN apk --no-cache update && apk --no-cache add --virtual build-dependencies \
    build-base alpine-sdk \
    fuse fuse-dev \
    automake autoconf git \
    libressl-dev \
    curl-dev libxml2-dev \
    ca-certificates \
    udev e2fsprogs  nvme-cli \
    e2fsprogs-extra \
    xfsprogs-extra

#RUN git clone https://github.com/s3fs-fuse/s3fs-fuse.git && \
#    cd s3fs-fuse \
#    git checkout tags/${S3FS_VERSION} && \
#    ./autogen.sh && \
#    ./configure --prefix=/usr && \
#    make && \
#    make install && \
#    s3fs --version && \
#    cd ../ && \
#    rm -rf s3fs-fuse

COPY --from=build-env /cds-csi-driver /cds-csi-driver

ENTRYPOINT ["/cds-csi-driver"]
