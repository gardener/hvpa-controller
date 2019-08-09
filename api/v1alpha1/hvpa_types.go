/*
Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VpaTemplateSpec defines the spec for VPA
type VpaTemplateSpec struct {
	// Controls how the autoscaler computes recommended resources.
	// The resource policy may be used to set constraints on the recommendations
	// for individual containers. If not specified, the autoscaler computes recommended
	// resources for all containers in the pod, without additional constraints.
	// +optional
	ResourcePolicy *vpa_api.PodResourcePolicy `json:"resourcePolicy,omitempty" protobuf:"bytes,3,opt,name=resourcePolicy"`
}

// HpaTemplateSpec defines the spec for HPA
type HpaTemplateSpec struct {
	// minReplicas is the lower limit for the number of replicas to which the autoscaler can scale down.
	// It defaults to 1 pod.
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty" protobuf:"varint,1,opt,name=minReplicas"`
	// maxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	// It cannot be less that minReplicas.
	MaxReplicas int32 `json:"maxReplicas" protobuf:"varint,2,opt,name=maxReplicas"`
	// metrics contains the specifications for which to use to calculate the
	// desired replica count (the maximum replica count across all metrics will
	// be used).  The desired replica count is calculated multiplying the
	// ratio between the target value and the current value by the current
	// number of pods.  Ergo, metrics used must decrease as the pod count is
	// increased, and vice-versa.  See the individual metric source types for
	// more information about how each type of metric must respond.
	// If not set, the default metric will be set to 80% average CPU utilization.
	// +optional
	Metrics []autoscaling.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,3,rep,name=metrics"`

	/*
		// lower limit for the number of pods that can be set by the autoscaler, default 1.
		// +optional
		MinReplicas *int32 `json:"minReplicas,omitempty" protobuf:"varint,1,opt,name=minReplicas"`
		// upper limit for the number of pods that can be set by the autoscaler; cannot be smaller than MinReplicas.
		MaxReplicas int32 `json:"maxReplicas" protobuf:"varint,2,opt,name=maxReplicas"`
		// target average CPU utilization (represented as a percentage of requested CPU) over all the pods;
		// if not specified the default autoscaling policy will be used.
		// +optional
		TargetCPUUtilizationPercentage *int32 `json:"targetCPUUtilizationPercentage,omitempty" protobuf:"varint,3,opt,name=targetCPUUtilizationPercentage"`
	*/
}

// WeightBasedScalingInterval defines the interval of replica counts in which VpaWeight is applied to VPA scaling
type WeightBasedScalingInterval struct {
	// VpaWeight defines the weight to be given to VPA's recommendationd for the interval of number of replicas provided
	VpaWeight VpaWeight `json:"vpaWeight,omitempty"`
	// StartReplicaCount is the number of replicas from which VpaWeight is applied to VPA scaling
	// If this field is not provided, it will default to minReplicas of HPA
	// +optional
	StartReplicaCount int32 `json:"startReplicaCount,omitempty"`
	// LastReplicaCount is the number of replicas till which VpaWeight is applied to VPA scaling
	// If this field is not provided, it will default to maxReplicas of HPA
	// +optional
	LastReplicaCount int32 `json:"lastReplicaCount,omitempty"`
}

// ScaleStabilization defines stabilization parameters after last scaling
type ScaleStabilization struct {
	// Duration defines the minimum delay in minutes between 2 consecutive scale operations
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h"
	Duration *string `json:"duration,omitempty"`
	// MinCpuChange is the minimum change in CPU on which HVPA acts
	// HVPA uses minimum of the Value and Percentage value
	MinCPUChange *ChangeThreshold `json:"minCpuChange,omitempty"`
	// MinMemChange is the minimum change in memory on which HVPA acts
	// HVPA uses minimum of the Value and Percentage value
	MinMemChange *ChangeThreshold `json:"minMemChange,omitempty"`
}

// HvpaSpec defines the desired state of Hvpa
type HvpaSpec struct {
	// HpaTemplate defines the spec of HPA
	HpaTemplate HpaTemplateSpec `json:"hpaTemplate,omitempty"`

	// VpaTemplate defines the spec of VPA
	VpaTemplate VpaTemplateSpec `json:"vpaTemplate,omitempty"`

	// WeightBasedScalingIntervals defines the intervals of replica counts, and the weights for scaling a deployment vertically
	// If there are overlapping intervals, then the vpaWeight will be taken from the first matching interval
	WeightBasedScalingIntervals []WeightBasedScalingInterval `json:"weightBasedScalingIntervals,omitempty"`

	// TargetRef points to the controller managing the set of pods for the autoscaler to control
	TargetRef *autoscaling.CrossVersionObjectReference `json:"targetRef"`

	// ScaleUpStabilization defines stabilization parameters after last scaling
	ScaleUpStabilization *ScaleStabilization `json:"scaleUpStabilization,omitempty"`

	// ScaleDownStabilization defines stabilization parameters after last scaling
	ScaleDownStabilization *ScaleStabilization `json:"scaleDownStabilization,omitempty"`
}

// ChangeThreshold defines the thresholds for HVPA to apply VPA's recommendations
type ChangeThreshold struct {
	// Value is the absolute value of the threshold
	// +optional
	Value *string `json:"value,omitempty"`
	// Percentage is the percentage of currently allocated value to be used as threshold
	// +optional
	Percentage *int32 `json:"percentage,omitempty"`
}

// VpaWeight - weight to provide to VPA scaling
type VpaWeight float32

const (
	// VpaOnly - only vertical scaling
	VpaOnly VpaWeight = 1.0
	// HpaOnly - only horizontal scaling
	HpaOnly VpaWeight = 0
)

// HvpaCurrentStatus defines the current status of HVPA
type HvpaCurrentStatus struct {
	HpaWeight VpaWeight `json:"hpaWeight,omitempty"`
	VpaWeight VpaWeight `json:"vpaWeight,omitempty"`
	// last time the HVPA scaled the resource;
	// used by the autoscaler to control how often the scaling is done.
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`
	// the kind of scaling that was done last time
	// +optional
	LastScaleType LastScaleType `json:"lastScaleType,omitempty"`
	// Override scale up stabilization window
	OverrideScaleUpStabilization bool `json:"overrideScaleUpStabilization,omitempty"`
}

// LastScaleType is the type of scaling
type LastScaleType struct {
	Horizontal Scaling `json:"horizontal,omitempty"`
	Vertical   Scaling `json:"vertical,omitempty"`
}

// Scaling defines the type of scaling
type Scaling string

const (
	// Down is scaling down vertically
	Down Scaling = "down"
	// Up is scaling up vertically
	Up Scaling = "up"
	// Out is scaling out horizontally
	Out Scaling = "out"
	// In is scaling in horizontally
	In Scaling = "in"
)

// HvpaStatus defines the observed state of Hvpa
type HvpaStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// current information about the HPA.
	// +optional
	HpaStatus HpaStatus `json:"hpaStatus,omitempty" protobuf:"bytes,1,opt,name=hpaStatus"`

	// Current information about the VPA.
	// +optional
	VpaStatus vpa_api.VerticalPodAutoscalerStatus `json:"vpaStatus,omitempty" protobuf:"bytes,2,opt,name=vpaStatus"`

	// Current information about HVPA.
	// +optional
	HvpaStatus HvpaCurrentStatus `json:"hvpaCurrentStatus,omitempty" protobuf:"bytes,3,opt,name=hvpaCurrentStatus"`
}

// HpaStatus defines the status of HPA
type HpaStatus struct {
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
}

// +kubebuilder:object:root=true

// Hvpa is the Schema for the hvpas API
type Hvpa struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HvpaSpec   `json:"spec,omitempty"`
	Status HvpaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HvpaList contains a list of Hvpa
type HvpaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hvpa `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hvpa{}, &HvpaList{})
}
