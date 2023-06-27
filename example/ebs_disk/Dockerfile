FROM golang:1.17 AS build-env
COPY . /go/src/github.com/capitalonline/eks-csi-driver
RUN go env -w GO111MODULE=on && go env -w GOPROXY=https://goproxy.cn,direct

RUN cd /go/src/github.com/capitalonline/eks-csi-driver && go mod tidy && CGO_ENABLED=0 GOARCH="amd64" GOOS="linux" go build -o /cds-csi-driver ./cmd/
#RUN cd /go/src/github.com/capitalonline/eks-csi-driver && go build -o /cds-csi-driver ./cmd/
#
FROM ubuntu:20.04

COPY --from=build-env /cds-csi-driver /cds-csi-driver

ENTRYPOINT ["/cds-csi-driver "]
