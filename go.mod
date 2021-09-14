module github.com/gardener/hvpa-controller

go 1.15

require (
	github.com/gardener/hvpa-controller/api v0.0.0
	github.com/golang/mock v1.4.1
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/prometheus/client_golang v1.11.0
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b
	gopkg.in/inf.v0 v0.9.1
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/autoscaler/vertical-pod-autoscaler v0.9.0
	k8s.io/client-go v0.21.3
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20210722164352-7f3ee0f31471
	sigs.k8s.io/controller-runtime v0.9.6
	sigs.k8s.io/controller-tools v0.4.0
)

replace github.com/gardener/hvpa-controller/api => ./api
