module github.com/gardener/hvpa-controller

go 1.15

require (
	github.com/gardener/hvpa-controller/api v0.0.0
	github.com/nxadm/tail v1.4.5 // indirect
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.1
	github.com/prometheus/client_golang v1.0.0
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b
	golang.org/x/net v0.0.0-20201021035429-f5854403a974
	golang.org/x/sys v0.0.0-20201107080550-4d91cf3a1aaf // indirect
	golang.org/x/tools v0.0.0-20201105220310-78b158585360 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/autoscaler/vertical-pod-autoscaler v0.9.0
	k8s.io/client-go v0.18.6
	k8s.io/klog v1.0.0
	sigs.k8s.io/controller-runtime v0.6.3
	sigs.k8s.io/controller-tools v0.4.0
)

replace github.com/gardener/hvpa-controller/api => ./api
