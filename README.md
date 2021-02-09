# hvpa-controller

[![CI Build status](https://concourse.ci.gardener.cloud/api/v1/teams/gardener/pipelines/hvpa-controller-master/jobs/master-head-update-job/badge)](https://concourse.ci.gardener.cloud/teams/gardener/pipelines/hvpa-controller-master/jobs/master-head-update-job)
[![Go Report Card](https://goreportcard.com/badge/github.com/gardener/hvpa-controller)](https://goreportcard.com/report/github.com/gardener/hvpa-controller)

### Goals
1. The goal of HVPA is to re-use the upstream components [HPA](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) and [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) as much as possible for scaling components horizontally and vertically respectively.
   1. HPA for recommendation and scaling horizontally.
   1. VPA for recommendation for scaling vertically.
1. Where there are gaps in using HPA and VPA simultaneously to scale a given component, introduce functionality to fill those gaps.
   1. HPA and VPA are recommended to be mixed only in case HPA is used for custom/external metrics as mentioned [here](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler#limitations-of-beta-version). But in some scenarios it might make sense to use them both even for CPU and memory (e.g. `kube-apiserver` or ingress etc.)
   1. VPA updates the pods directly (via webhooks) which may confuse HPA (especially, when used for the same scaling metrics) as it may not see the changes in the upstream `targetRefs`.
1. Where there is functionality missing in either HPA or VPA, introduce them to provide more flexibility during horizontal and vertical scaling. Especially, for the components that experience disruption during scaling.
   1. Horizontally and vertically silmultaneously
   1. Support for configurable (at the resource level) threshold levels to trigger VPA (and possibly HPA) updates to minimize unnecessary scaling of components. Especially, if scaling is disruptive
   1. Support for configurable (at the resource level) stabilisation time window in both the directions (up/down) to stabilize scaling of components. Especially, if scaling is disruptive
   1. Support for configurable maintenance window for scaling (especially, scaling in/down) for components that do not scale well smoothly (mainly, `etcd`, but to a lesser extent `kube-apiserver` as well for `WATCH` requests). This could be as an alternative or complementary to the stabilisation window mentioned above
   1. Custom resource support for VPA updates.
      * At the beginning, this would be with some well-defined annotations to describe which part of the CRD to update the resource requests.
      * But eventually, we can think of proposing a `Resource` subresource (per container) along the lines of `Scale` subresource.


### Non-goals

* It is *not* a goal of HVPA to duplicate the functionality of the upstream components like [HPA](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscaler/) and [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler).

### Design of HVPA Controller
See the design documentation in the `./docs/` [here](docs/proposals/HVPA-EP-0001/HVPA-EP-001.md).

### Custom Resource Definition
:construction: Work in progress

#### Types

 [DEPRECATED] See [here](https://github.com/gardener/hvpa-controller/blob/a336e86111bbd5ec87f5a749003ba39b4735261e/api/v1alpha1/hvpa_types.go#L28-L305).

#### Example

[DEPRECATED] See [here](https://github.com/gardener/hvpa-controller/blob/a698b08bc371525f666bb2f4766ccb9a403a0d4b/config/samples/autoscaling_v1alpha1_hvpa.yaml).