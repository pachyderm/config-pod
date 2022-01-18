module github.com/pachyderm/config-pod

go 1.15

require (
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/jackc/pgx/v4 v4.13.0 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/pachyderm/pachyderm/v2 v2.0.5
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210614182718-04defd469f4e // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	golang.org/x/tools v0.1.4 // indirect
	gopkg.in/ini.v1 v1.62.0 // indirect
	honnef.co/go/tools v0.1.4 // indirect
)

replace github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190718183610-8e956561bbf5

replace github.com/sercand/kuberesolver => github.com/sercand/kuberesolver v1.0.1-0.20200204133151-f60278fd3dac

// Dex pulls in a newer grpc and protobuf, but our etcd client can't work with the newer version.
// The following pin grpc, protobuf and everything else that would otherwise rely on the newer version.
// See https://github.com/etcd-io/etcd/pull/12000
replace google.golang.org/grpc => google.golang.org/grpc v1.29.1

replace github.com/golang/protobuf => github.com/golang/protobuf v1.3.5

replace cloud.google.com/go => cloud.google.com/go v0.49.0

replace cloud.google.com/go/storage => cloud.google.com/go/storage v1.10.0

replace github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.5.0

replace github.com/prometheus/common => github.com/prometheus/common v0.9.1

replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20191115194625-c23dd37a84c9

replace github.com/dexidp/dex => github.com/pachyderm/dex v0.0.0-20210811182333-56fc504b721f
