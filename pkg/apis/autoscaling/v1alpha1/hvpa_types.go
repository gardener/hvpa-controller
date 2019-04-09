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

// HvpaSpec defines the desired state of Hvpa
type HvpaSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	// Scaling of a deployment is divided into 3 stages.
	// Stage 1 starts at HPA's minReplicas
	// Stage 3 ends at HPA's maxReplicas
	// When in stage 2, weight given to VPA's recommendation will be SecondStageVpaWeight
	// StageTwoStartReplicaCount defines the number of replicas when stage 2 starts
	// +optional
	StageTwoStartReplicaCount int `json:"StageTwoStartReplicaCount,omitempty"`

	// StageTwoStopReplicaCount defines the number of replicas when stage 2 stops
	// +optional
	StageTwoStopReplicaCount int `json:"StageTwoStopReplicaCount,omitempty"`

	// FirstStageVpaWeight defines the weight to be given VPA recommendations in stage 1
	FirstStageVpaWeight VpaWeight `json:"FirstStageVpaWeight,omitempty"`

	// SecondStageVpaWeight defines the weight to be given VPA recommendations in stage 2
	SecondStageVpaWeight VpaWeight `json:"SecondStageVpaWeight,omitempty"`

	// ThirdStageVpaWeight defines the weight to be given VPA recommendations in stage 3
	ThirdStageVpaWeight VpaWeight `json:"ThirdStageVpaWeight,omitempty"`

	// HpaSpec defines the spec of HPA
	HpaSpec scaling_v1.HorizontalPodAutoscaler `json:"hpaSpec,omitempty"`

	// VpaSpec defines the spec of VPA
	VpaSpec vpa_api.VerticalPodAutoscaler `json:"vpaSpec,omitempty"`
}

// VpaWeight - weight to provide to VPA scaling
type VpaWeight float32

const (
	// VpaOnly - only vertical scaling
	VpaOnly VpaWeight = 1.0
	// HpaOnly - only horizontal scaling
	HpaOnly VpaWeight = 0
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
