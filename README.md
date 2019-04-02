# hvpa-controller

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
[Here](https://github.com/ggaurav10/hvpa-controller/blob/master/pkg/apis/autoscaling/v1alpha1/hvpa_types.go) is the spec for HVPA controller

Example 1:

```yaml
apiVersion: autoscaling.k8s.io/v1alpha1
kind: Hvpa
metadata:
  name: hvpa-sample
spec:
  scaleUpStabilizationWindow: "2m"
  scaleDownStabilizationWindow : "3m"
  minCpuChange:
    value: "200m"
    percentage: 70
  minMemChange:
    value: "200M"
    percentage: 80
  hpaTemplate:
    maxReplicas: 10
    minReplicas: 1
    metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 50
  vpaTemplate:
    resourcePolicy:
      containerPolicies:
      - MinAllowed:
          memory: 400Mi
        containerName: resource-consumer
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
  hpaTemplate:
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
  hpaTemplate:
    minReplicas: 1
    maxReplicas: 10
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