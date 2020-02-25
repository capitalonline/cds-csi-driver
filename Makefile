PKG=github.com/capitalonline/cds-csi-driver
IMAGE?=registry-bj.capitalonline.net/cck/cds-csi-driver
VERSION=v0.1.0
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/common.version=${VERSION} -X ${PKG}/pkg/common.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/common.buildDate=${BUILD_DATE} -s -w"

.EXPORT_ALL_VARIABLES:

.PHONY: build
build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags ${LDFLAGS} -o bin/cds-csi-driver ./cmd/

.PHONY: container-binary
container-binary:
	CGO_ENABLED=0 GOARCH="amd64" GOOS="linux" go build -ldflags ${LDFLAGS} -o /cds-csi-driver ./cmd/

.PHONY: image-release
image-release:
	docker build -t $(IMAGE):$(VERSION) .

.PHONY: image
image:
	docker build -t $(IMAGE):latest .

.PHONY: push-release
push-release:
	docker push $(IMAGE):$(VERSION)

.PHONY: test
test:
	go test -v -race ./pkg/...
