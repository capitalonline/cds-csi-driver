module github.com/capitalonline/cds-csi-driver

go 1.18

require (
	github.com/capitalonline/cck-sdk-go v2.2.2+incompatible
	github.com/container-storage-interface/spec v1.2.0
	github.com/kubernetes-csi/csi-lib-utils v0.7.0 // indirect
	github.com/kubernetes-csi/drivers v1.0.2
	github.com/sirupsen/logrus v1.4.2
	github.com/wxnacy/wgo v1.0.4 // indirect
	golang.org/x/net v0.0.0-20200114155413-6afb5195e5aa // indirect
	golang.org/x/sys v0.0.0-20200926100807-9d91bd62050c // indirect
	google.golang.org/grpc v1.26.0
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.1-beta.0
	k8s.io/client-go v0.17.0
)

replace (
	github.com/capitalonline/cck-sdk-go v2.2.2+incompatible => ../cck-sdk-go
)