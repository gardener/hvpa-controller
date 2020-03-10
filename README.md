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
   1. [Weight-based scaling](#weight-based-scaling) horizontally and vertically silmultaneously.
   1. Support for configurable (at the resource level) threshold levels to trigger VPA (and possibly HPA) updates to minimize unnecessary scaling of components. Especially, if scaling is disruptive.
   1. Support for configurable (at the resource level) stabilisation window in all the four directions (up/down/out/in) to stabilize scaling of components. Especially, if scaling is disruptive.
   1. Support for configurable maintenance window for scaling (especially, scaling in/down) for components that do not scale well smoothly (mainly, `etcd`, but to a lesser extent `kube-apiserver` as well for `WATCH` requests). This could be as an alternative or complementary to the stabilisation window mentioned above.
   1. Support for flexible update policy for all four scaling directions (Off/Auto/ScaleUp). ScaleUp would only apply scale up and not scale down (vertically or horizontally). This is again from the perspective of components which experience disruption while scaling (mainly, `etcd`, but to a lesser extent `kube-apiserver` as well for `WATCH` requests). For such components, a `ScaleUp` update policy will ensure that the component can scale up (with some disruption) automatically to meet the workload requirement but not scale down to avoid unnecessary disruption. This would mean over-provisioning for workloads that experience a short upsurge.
   1. Custom resource support for VPA updates.
      * At the beginning, this would be with some well-defined annotations to describe which part of the CRD to update the resource requests.
      * But eventually, we can think of proposing a `Resource` subresource (per container) along the lines of `Scale` subresource.


### Non-goals

* It is *not* a goal of HVPA to duplicate the functionality of the upstream components like [HPA](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscaler/) and [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler).


### Weight based scaling
In this mode, deployment's scaling is divided into stages depending on number of replicas. A user can provide an array of intervals `x1` to `x2` and corresponding `vpaWeight`s between and including `0` and `1`. `x1` and `x2` are the number of replicas where that stage begins and ends respectively. In a particular stage, HVPA controller will consider VPA's recommendations according to the weights provided, and scale the deployment accordingly. The weights given to the HPA's recommendation will be `1 - vpaWeight`.

According to VPA, new `requests` will be `currentRequest + (targetRequest - currentRequest) * vpaWeight`

According to HPA, new number of replicas will be:
* ceil of `currentReplicas + (desiredReplicas - currentReplicas) * (1 - vpaWeight)` if desired replicas is more than current replicas
* floor of `currentReplicas + (desiredReplicas - currentReplicas) * (1 - vpaWeight)` if desired replicas is less than current replicas

`vpaWeight` = 1 will result in pure vertical scaling
`vpaWeight` = 0 will result in pure horizontal scaling

```
Resource request vs number of replicas curve could typically look like:
Here First stage Vpa weight = 0, second stage Vpa weight is a fraction, third stage Vpa weight = 0

resource
request
^
|
|                  ____________
|                 /|
|                /
|               /  |
|              /
|  ___________/    |
|
|-------------|----|-----------|----->
   min        x1    x2        max     #Replicas
```

```
Example of vpaWeight vs number of replicas.
Here first stage Vpa weight = 0, second stage Vpa weight = 0.4, third stage Vpa weight = 0

vpaWeight
   ^
   |
0.4|           |-------|
   |           |       |
  0|   ________|       |____________
   |------------------------------------->
               x1     x2               #Replicas
```

**Scaling of `limits`**:

HVPA will scale `limits` also along with `requests` based on following criteria:
1. If originalLimit is not set, don't set limit.
1. If originalLimit is set but originalRequest not set - K8s will treat the pod as if they were equal, then set limit equal to request
1. If originalLimit and originalRequest are set and if they are equal, recommend limit equal to request.
1. If limit scaling parameters are not set in `hvpa.spec.vpa` then scale the limits proportionaly as done by VPA
1. If the scaling parameters are provided, then choose the min of the 2 possible values in `hvpa.spec.vpa.limitsRequestsGapScaleParams` (percentage and value)

#### Pros
* Works even if HPA and VPA act on different metrices
* Gives better control to user on scaling of apps
#### Cons
* Need to define behavior when:
    * current number of replicas is `x2`, the deployment has not yet scaled up to `maxAllowed`, and user has chosen to scale only horizontally after `x2`. In this case, the deployment cannot scale up vertically anymore if the load increases.
    * current number of replicas is `x1`, the deployment has not yet scaled down to `minAllowed`, and user has chosen to scale only horizontally for lower values than `x1`. In this case, the deployment cannot scale down vertically anymore if the load decreases.
* Will need both VPA and HPA to be deployed in a recommendation-only mode.

#### Mitigation
* `x1` and `x2` will be optional, If either or both of them are not provided, vertical scaling will be done until `minAllowed` and/or `maxAllowed` correspondingly.
* Once replica count reaches `maxReplicas`, and VPA still recommends higher resource requirements, then vertical scaling will be done unconditionally

#### Spec
[Here](#types) is the spec for HVPA controller

Example 1:

```yaml
apiVersion: autoscaling.k8s.io/v1alpha1
kind: Hvpa
metadata:
  name: hvpa-sample
spec:
  maintenanceTimeWindow:
    begin: "220000-0000"
    end:  "230000-0000"
  hpa:
    deploy: true
    selector:
      matchLabels:
        key1: value1
    scaleUp:
      updatePolicy:
        updateMode: "Auto"
      stabilizationDuration: "2m"
    scaleDown:
      updatePolicy:
        updateMode: "Auto"
      stabilizationDuration: "3m"
    template:
      metadata:
        labels:
          key1: value1
      spec:
        maxReplicas: 10
        minReplicas: 1
        metrics:
        - resource:
            name: memory
            targetAverageUtilization: 50
          type: Resource
        - resource:
            name: cpu
            targetAverageUtilization: 50
          type: Resource
 vpa:
    deploy: true
    selector:
      matchLabels:
        key2: value2
    scaleUp:
      updatePolicy:
        updateMode: "Auto"
      stabilizationDuration: "2m"
      minChange:
        cpu:
          value: "3"
          percentage: 80
        memory:
          value: "3"
          percentage: 80
    scaleDown:
      updatePolicy:
        updateMode: "MaintenanceWindow"
      stabilizationDuration: "3m"
      minChange:
        cpu:
          value: "3"
          percentage: 80
        memory:
          value: "3"
          percentage: 80
    limitsRequestsGapScaleParams:
      cpu:
        percentage: 80
        value: "2"
      memory:
        percentage: 80
        value: 3G
    template:
      metadata:
        labels:
          key2: value2
      spec:
        resourcePolicy:
          containerPolicies:
            - containerName: resource-consumer
              minAllowed:
                memory: 400Mi
              maxAllowed:
                memory: 3000Mi
  weightBasedScalingIntervals:
    - vpaWeight: 0
      startReplicaCount: 1
      lastReplicaCount: 2
    - vpaWeight: 0.6
      startReplicaCount: 3
      lastReplicaCount: 7
    - vpaWeight: 0
      startReplicaCount: 7
      lastReplicaCount: 10
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: resource-consumer
```
```
resource
request
^
|
|                  _________
|                 /|
|                /
|               /  |
|              /
|  ___________/    |
|
|-------------|----|--------|----->
   1          3    7        10     #Replicas
```


Example 2:
```yaml
apiVersion: autoscaling.k8s.io/v1alpha1
kind: Hvpa
metadata:
  name: hvpa-sample
spec:
  weightBasedScalingIntervals:
    - vpaWeight: 1
      startReplicaCount: 1
      lastReplicaCount: 1
    - vpaWeight: 0.6
      startReplicaCount: 2
      lastReplicaCount: 4
    - vpaWeight: 0
      startReplicaCount: 5
      lastReplicaCount: 10
  hpa:
    .
    .
    .
```
```
resource
request
^
|       ______
|      /
|     /  
|    /
|   /  
|  |
|  |  
|  |
|
|--|----|-----|------->
   1   4       10     #Replicas
```


Example 3: After maxReplicas, even if weight is not mentioned, there is only vertical scaling because HPA is limited by maxReplicas
```yaml
apiVersion: autoscaling.k8s.io/v1alpha1
kind: Hvpa
metadata:
  name: hvpa-sample
spec:
  weightBasedScalingIntervals:
    - vpaWeight: 0
      startReplicaCount: 1
      lastReplicaCount: 3
    - vpaWeight: 0.6
      startReplicaCount: 4
      lastReplicaCount: 10
  hpa:
    .
    .
    template:
      .
      .
      spec:
        minReplicas: 1
        maxReplicas: 10
        metrics:
        .
        .
    .
    .
    .
```
```
resource
request
^
|
|           |
|           |
|           .     
|          /
|         /
|        / 
|       /
|  ____/ 
|
|--|---|----|------>
   1   3    10     #Replicas
```

### Custom Resource Definition

#### Types

See [here](https://github.com/gardener/hvpa-controller/blob/a336e86111bbd5ec87f5a749003ba39b4735261e/api/v1alpha1/hvpa_types.go#L28-L305).

#### Example

See [here](https://github.com/gardener/hvpa-controller/blob/a698b08bc371525f666bb2f4766ccb9a403a0d4b/config/samples/autoscaling_v1alpha1_hvpa.yaml).