# HVPA controller

## Goal
A pod can be scaled horizontally by deploying more replicas of the pod with the same values of “request” and “limit” parameters, or can be scaled vertically by increasing the “request” parameter.
Currently, separate components – Horizontal Pod Autoscaler and Vertical Pod Autoscaler  - take care of scaling an application running inside a pod. These two components run independent of each other. Scaling by one of the components may disrupt the calculations for the other, which may result in destablising the application.

The proposal is to develop an HVPA controller. It takes only the scaling recommendations from both the pod autoscalers, however, applies those recommendations on its own so that horizontal and vertical scaling can be done in tandem.

A minor goal is to make it also possible to use HVPA only with VPA (without HPA). In this mode, HVPA can be used as an alternative updater for VPA where the upstream resources are updated directly instead of updating the `pods` directly.

## Non-goal

It is not a goal of HVPA to replace Horizontal Pod Autoscaler and Vertical Pod Autoscaler. It proposes to make use of HPA and VPA components (with possible small changes) for recommendations for scaling while taking more control of actually applying the recommendations.

## Modes in which recommendation can be applied:

Most of these modes will require at least one if not both of the pod autoscalers (HPA and VPA) to be provisioned with only recommendations switched on.

VPA already supports this with [`UpdateMode: Off`](https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2/types.go#L105).

HPA will have to be enhanced to support simlar recommendation-only mode.

### HPA preferred
When scaling, pods are scaled vertically up only after they are scaled to maximum number of replicas possible. Also, they are scaled vertically down only after they are scaled to minimum number of replicas possible.

#### Pros:
* Easy to implement for cases where HPA and VPA act on same metrices
* Vertical scaling results in rolling update, which may not be ideal for some applications. In this mode, vertical scaling is done only when horizontal scaling is not possible anymore
* Only VPA needs to be deployed with `UpdateMode: Off`. HPA can be deployed noremally.

#### Cons:
* If HPA and VPA act on different metrices `m1` and `m2` respectively, then there may be a case when according to `m1`, horizontal scaling is not required, however, according to `m2` vertical scaling is required. In such a case, there will not be any kind of scaling.

### HPA biased
In case both HPA and VPA have recommendations to scale, HVPA controller will always apply HPA's recommendations. VPA's recommendation will be applied only when HPA doesn't have any recommendation.

#### Pros:
* Works even if HPA and VPA act on different metrices
* Vertical scaling is done only when required
#### Cons:
* Might need both VPA and HPA to be deployed in a recommendation-only mode.

### VPA biased
In case both HPA and VPA have recommendations to scale, HVPA controller will always apply VPA's recommendations. HPA's recommendation will be applied only when VPA doesn't have any recommendation.

#### Pros:
* Works even if HPA and VPA act on different metrices
#### Cons:
* May have more frequent rolling updates. Can it be mitigated by setting user provided thresholds for vertical autoscaling?
* Does it make sense to give preference to VPA instead of HPA, when vertical scaling may result in rolling updates?
* Will need both VPA and HPA to be deployed in a recommendation-only mode.

### Mixed
In this mode, along with the preference for autoscaling (HPA or VPA), user will also provide thresholds for horizontal and vertical autoscaling, say, `x` and `y` respectively. If only one autoscaler has a new recommendation, it will be applied. If both the components have recommendations:
* HPA's recommendation will be applied if the difference between the recommended replicas and the current replicas is greater than `x`.
* VPA's recommendation will be applied if the difference between the recommended value and the current value is greater than `y`, or the recommended value is more than the `limit`.
* In case both above conditions are satisifed, user provided preference will be taken into account

#### Pros:
* Works even if HPA and VPA act on different metrices
* Disruptions because of rolling updates can be controlled as rolling updates will be applied less frequently because of higher threshold
* Provides more control to users for autoscaling apps
* Relies on users' understanding of application, and its scaling
#### Cons:
* Relies on users' understanding of application, and its scaling
* Will need both VPA and HPA to be deployed in a recommendation-only mode.

### Ratio based scaling
In this mode, user can provide an interval `x1` to `x2` and a ratio. When the number of replicas for a deployment is between `x1` and `x2`, HVPA controller will consider VPA's and HPA's recommendations in the ratio provided, and scale the deployment accordingly.
User will also provide the choice of scaling when the number of replicas is less than `x1`, and greater than `x2`.

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

#### Spec

```golang
// RatioBasedScaling defines spec for ratio based scaling
type RatioBasedScaling struct {
	// VtoHScalingRatio defines the ratio in which VPA's and HPA's recommendations should be applied
	VtoHScalingRatio float32 `json:"vToHScalingRatio,omitempty"`

	// StartReplicaCount is the number of replicas after which ratio based scaling starts
	// +optional
	StartReplicaCount int `json:"startReplicaCount,omitempty"`

	// FinishReplicaCount is the number of replicas after which ratio based scaling stops
	// +optional
	FinishReplicaCount int `json:"finishReplicaCount,omitempty"`
}

// HvpaSpec defines the desired state of Hvpa
type HvpaSpec struct {
	// InitialScaling defines the scaling preference before StartReplicaCount
	InitialScaling ScalingType `json:"initialScaling,omitempty"`

	// FinalScaling defines the scaling preference after FinishReplicaCount
	FinalScaling ScalingType `json:"finalScaling,omitempty"`

	//RatioBasedScalingSpec defines the spec for weightage based horizontal and vertical scaling
	RatioBasedScalingSpec RatioBasedScaling `json:"ratioBasedScalingSpec,omitempty"`

	// HpaSpec defines the spec of HPA
	HpaSpec scaling_v1.HorizontalPodAutoscaler `json:"hpaSpec,omitempty"`

	// VpaSpec defines the spec of VPA
	VpaSpec vpa_api.VerticalPodAutoscaler `json:"vpaSpec,omitempty"`
}

// ScalingType - horizontal or vertical
type ScalingType string

const (
	// Vertical scaling
	Vertical ScalingType = "vertical"
	// Horizontal scaling
	Horizontal ScalingType = "horizontal"
)
```

## Currently preferred approach
### Ratio based scaling