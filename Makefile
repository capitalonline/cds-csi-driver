PKG=github.com/capitalonline/cds-csi-driver
IMAGE?=registry-bj.capitalonline.net/cck/cds-csi-driver
IMAGE_OVERSEA=capitalonline/cds-csi-driver
VERSION=v2.0.4
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/common.version=${VERSION} -X ${PKG}/pkg/common.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/common.buildDate=${BUILD_DATE} -s -w"
NAS_DEPLOY_PATH=./deploy/nas
NAS_KUSTOMIZATION_RELEASE_PATH=${NAS_DEPLOY_PATH}/overlays/release
NAS_KUSTOMIZATION_TEST_PATH=${NAS_DEPLOY_PATH}/overlays/test
NAS_KUSTOMIZATION_FILE=${NAS_KUSTOMIZATION_RELEASE_PATH}/kustomization.yaml
OSS_DEPLOY_PATH=./deploy/oss
OSS_KUSTOMIZATION_RELEASE_PATH=${OSS_DEPLOY_PATH}/overlays/release
OSS_KUSTOMIZATION_TEST_PATH=${OSS_DEPLOY_PATH}/overlays/test
OSS_KUSTOMIZATION_FILE=${OSS_KUSTOMIZATION_RELEASE_PATH}/kustomization.yaml
DISK_DEPLOY_PATH=./deploy/disk
DISK_KUSTOMIZATION_RELEASE_PATH=${DISK_DEPLOY_PATH}/overlays/release
DISK_KUSTOMIZATION_TEST_PATH=${DISK_DEPLOY_PATH}/overlays/test
DISK_KUSTOMIZATION_FILE=${DISK_KUSTOMIZATION_RELEASE_PATH}/kustomization.yaml
BLOCK_DEPLOY_PATH=./deploy/block
BLOCK_KUSTOMIZATION_RELEASE_PATH=${BLOCK_DEPLOY_PATH}/overlays/release
BLOCK_KUSTOMIZATION_TEST_PATH=${BLOCK_DEPLOY_PATH}/overlays/test
BLOCK_KUSTOMIZATION_FILE=${BLOCK_KUSTOMIZATION_RELEASE_PATH}/kustomization.yaml
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
	docker tag $(IMAGE):$(VERSION) $(IMAGE_OVERSEA):$(VERSION)

.PHONY: image
image:
	docker build -t $(IMAGE):latest .

.PHONY: release
release: image-release
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE_OVERSEA):$(VERSION)

.PHONY: sync-version
sync-version:
	sed -i.bak 's/newTag: .*/newTag: '${VERSION}'/g' ${NAS_KUSTOMIZATION_FILE} && rm ${NAS_KUSTOMIZATION_FILE}.bak
	sed -i.bak 's/newTag: .*/newTag: '${VERSION}'/g' ${OSS_KUSTOMIZATION_FILE} && rm ${OSS_KUSTOMIZATION_FILE}.bak
	sed -i.bak 's/newTag: .*/newTag: '${VERSION}'/g' ${DISK_KUSTOMIZATION_FILE} && rm ${DISK_KUSTOMIZATION_FILE}.bak
	sed -i.bak 's/newTag: .*/newTag: '${VERSION}'/g' ${BLOCK_KUSTOMIZATION_FILE} && rm ${BLOCK_KUSTOMIZATION_FILE}.bak

.PHONY: kustomize
kustomize:sync-version
	kubectl kustomize ${NAS_KUSTOMIZATION_RELEASE_PATH} > ${NAS_DEPLOY_PATH}/deploy.yaml
	kubectl kustomize ${OSS_KUSTOMIZATION_RELEASE_PATH} > ${OSS_DEPLOY_PATH}/deploy.yaml
	kubectl kustomize ${DISK_KUSTOMIZATION_RELEASE_PATH} > ${DISK_DEPLOY_PATH}/deploy.yaml
	kubectl kustomize ${BLOCK_KUSTOMIZATION_RELEASE_PATH} > ${BLOCK_DEPLOY_PATH}/deploy.yaml

.PHONY: unit-test
unit-test:
	@echo "**************************** running unit test ****************************"
	go test -v -race ./pkg/...

.PHONY: test-prerequisite
test-prerequisite:
	docker build -t $(IMAGE):test . && docker push $(IMAGE):test
	kubectl kustomize ${NAS_KUSTOMIZATION_TEST_PATH} | kubectl apply -f -
	kubectl kustomize ${OSS_KUSTOMIZATION_TEST_PATH} | kubectl apply -f -
	kubectl kustomize ${DISK_KUSTOMIZATION_TEST_PATH} | kubectl apply -f -
	kubectl kustomize ${BLOCK_KUSTOMIZATION_TEST_PATH} | kubectl apply -f -

.PHONY: integration-test
integration-test:
	@echo "**************************** running integration test ****************************"
	@./test.sh

.PHONE: test
test: unit-test integration-test
	@echo "**************************** all tests passed ****************************"

.PHONE: oss-test
oss-test:
	@echo "**************************** running oss unit test ****************************"
	go test -v -race ./pkg/driver/oss/...
	@echo "**************************** running oss integration test ****************************"
	@./test/oss/test.sh
	@echo "**************************** all tests passed ****************************"

.PHONE: block-test
block-test:
	@echo "**************************** running oss unit test ****************************"
	go test -v -race ./pkg/driver/block/...
	@echo "**************************** running oss integration test ****************************"
	@./test/block/test.sh
	@echo "**************************** all tests passed ****************************"
