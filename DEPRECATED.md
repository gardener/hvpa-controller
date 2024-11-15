# Deprecation Notice

The hvpa-controller is deprecated. It will be no longer maintained by the Gardener team.
This document describes describes the alternatives for existing features in HVPA.

### Known Issues

The [Known Issues](docs/KnownIssues.md) document describes the known issues with the hvpa-controller.

### Alternatives

##### Alternatives for the "disable scaling down" functionality

HVPA has several options to disable scaling down:
- Disable scaling down completely, only allow scaling up. This is possible by using `.spec.(hpa/vpa).scaleDown.updatePolicy.updateMode=Off`.
- Disable scaling down outside of maintenance window.
- Disable scaling down for a period of time after the last scaling happened.
- Disable scaling down if a minimum difference in absolute/percentage hasn't been reached.

The alternative for "Disable scaling down completely, only allow scaling up." is implemented in the [Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master) with [KEP-4831](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler/enhancements/4831-control-eviction-behavior).

Example VPA resource that disables scaling down but allows scaling up:
```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
# ...
spec:
  # ...
  updatePolicy:
    evictionRequirements:
    - changeRequirement: TargetHigherThanRequests
      resources:
      - memory
      - cpu
    updateMode: Auto
```

The alternative for "Disable scaling down outside of maintenance window." is to use the [gardenlet's `VPAEvictionRequirements` controller](https://github.com/gardener/gardener/blob/master/docs/concepts/gardenlet.md#vpaevictionrequirements-controller).

There is no direct alternative for "Disable scaling down for a period of time after the last scaling happened.". However, it can be implemented externally using the VPA's eviction requirements in a similar way how [gardenlet's `VPAEvictionRequirements` controller](https://github.com/gardener/gardener/blob/master/docs/concepts/gardenlet.md#vpaevictionrequirements-controller) is implemented.

There is no direct alternative for "Disable scaling down if a minimum difference in absolute/percentage hasn't been reached.". However, this functionality can lead to cases where the configured `minChange` value hasn't been reached but the affected component is being for example CPU throttled due to 100% CPU usage on the Node.

##### Alternative for combining HPA and VPA

From [Vertical Pod Autoscaler's Known Limitation section](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler#known-limitations):
>  Vertical Pod Autoscaler **should not be used with the [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#support-for-resource-metrics) (HPA) on the same resource metric (CPU or memory)** at this moment. However, you can use [VPA with HPA on separate resource metrics](https://github.com/kubernetes/autoscaler/issues/6247) (e.g. VPA on memory and HPA on CPU) as well as with [HPA on custom and external metrics](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#scaling-on-custom-metrics).

There is pod-trashing cycle between VPA and HPA scaling on the same metric when HPA is configured to scale **on average utilization**. 

The main issue is that once HPA has scaled, VPA scales as well and vice versa, because they both influence themselves (through the requests) when utilization is the target for HPA, e.g. if usage increases and then replicas increase, utilization per requests/replica decreases (goal of HPA), but then recommendations and therefore requests (usually) decrease to match the new usage (goal of VPA), which leads to a higher utilization again and triggers the increase of replicas again.

Luckily, it is possible to break that cycle and the solution is to NOT scale on the average utilization, but **on the average usage**. See more details in [Combining HPA and VPA section](https://github.com/gardener/gardener/blob/master/docs/usage/autoscaling/shoot_pod_autoscaling_best_practices.md#combining-hpa-and-vpa).
