module github.com/capitalonline/cds-csi-driver

go 1.18

require (
	github.com/capitalonline/cck-sdk-go v2.2.2+incompatible
	github.com/container-storage-interface/spec v1.2.0
	github.com/getsentry/sentry-go v0.22.0
	github.com/kubernetes-csi/drivers v1.0.2
	github.com/sirupsen/logrus v1.9.3
	github.com/wxnacy/wgo v1.0.4
	golang.org/x/net v0.8.0
	golang.org/x/sys v0.6.0
	google.golang.org/grpc v1.26.0
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.1-beta.0
	k8s.io/client-go v0.17.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gogo/protobuf v1.2.2-0.20190723190241-65acae22fc9d // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/protobuf v1.3.2 // indirect
	github.com/google/gofuzz v1.0.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.7.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/term v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	google.golang.org/appengine v1.5.0 // indirect
	google.golang.org/genproto v0.0.0-20191220175831-5c49e3ecc1c1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f // indirect
	sigs.k8s.io/yaml v1.1.0 // indirect
)

replace github.com/capitalonline/cck-sdk-go v2.2.2+incompatible => ../cck-sdk-go
