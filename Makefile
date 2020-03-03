PKG=github.com/capitalonline/cds-csi-driver
IMAGE?=registry-bj.capitalonline.net/cck/cds-csi-driver
VERSION=v0.1.0
GIT_COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS?="-X ${PKG}/pkg/common.version=${VERSION} -X ${PKG}/pkg/common.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/common.buildDate=${BUILD_DATE} -s -w"
NAS_DEPLOY_PATH=./deploy/nas
NAS_KUSTOMIZATION_RELEASE_PATH=${NAS_DEPLOY_PATH}/overlays/release
NAS_KUSTOMIZATION_TEST_PATH=${NAS_DEPLOY_PATH}/overlays/test
NAS_KUSTOMIZATION_FILE=${NAS_KUSTOMIZATION_PATH}/kustomization.yaml
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

.PHONY: release
release: image-release
	docker push $(IMAGE):$(VERSION)

.PHONY: sync-version
sync-version:
	sed -i.bak 's/newTag: .*/newTag: '${VERSION}'/g' ${NAS_KUSTOMIZATION_FILE} && rm ${NAS_KUSTOMIZATION_FILE}.bak

.PHONY: kustomize
kustomize:sync-version
	kubectl kustomize ${NAS_KUSTOMIZATION_RELEASE_PATH} > ${NAS_DEPLOY_PATH}/deploy.yaml

.PHONY: unit-test
unit-test:
	@echo "**************************** running unit test ****************************"
	go test -v -race ./pkg/...

.PHONY: test-prerequisite
test-prerequisite:
	docker build -t $(IMAGE):test . && docker push $(IMAGE):test
	kubectl kustomize ${NAS_KUSTOMIZATION_TEST_PATH} | kubectl apply -f -

.PHONY: integration-test
integration-test:
	@echo "**************************** running integration test ****************************"
	@./test.sh

.PHONE: test
test: unit-test integration-test
	@echo "**************************** all tests passed ****************************"
