/*
Copyright 2019 Gaurav Gupta.
*/

package v1alpha1

import (
	scaling_v1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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
	// Important: Run "make" to regenerate code after modifying this file
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

// HvpaStatus defines the observed state of Hvpa
type HvpaStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Hvpa is the Schema for the hvpas API
// +k8s:openapi-gen=true
type Hvpa struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HvpaSpec   `json:"spec,omitempty"`
	Status HvpaStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HvpaList contains a list of Hvpa
type HvpaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hvpa `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hvpa{}, &HvpaList{})
}
