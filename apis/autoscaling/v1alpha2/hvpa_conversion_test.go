package v1alpha2_test

import (
	"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("HvpaConversion", func() {
	name := "hvpa-10"
	target := "scaling-target"
	labelVal := "hpa-label"
	updateMode := v1alpha1.UpdateModeAuto
	stabilizationDur := "3m"
	replica := int32(1)
	util := int32(70)

	valMem := "100M"
	valCPU := "100m"
	percMem := int32(80)
	percCPU := int32(80)

	statusReplicas := int32(7)
	t := metav1.Now()

	minChange := v1alpha1.ScaleParams{
		CPU: v1alpha1.ChangeParams{
			Value:      &valCPU,
			Percentage: &percCPU,
		},
		Memory: v1alpha1.ChangeParams{
			Value:      &valMem,
			Percentage: &percMem,
		},
	}

	vpaRecommendation := vpa_api.RecommendedPodResources{
		ContainerRecommendations: []vpa_api.RecommendedContainerResources{{
			ContainerName: "some-container",
			Target: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("340001k"),
				v1.ResourceCPU:    resource.MustParse("201m"),
			},
			LowerBound: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("340002k"),
				v1.ResourceCPU:    resource.MustParse("202m"),
			},
			UpperBound: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("340003k"),
				v1.ResourceCPU:    resource.MustParse("203m"),
			},
			UncappedTarget: v1.ResourceList{
				v1.ResourceMemory: resource.MustParse("340004k"),
				v1.ResourceCPU:    resource.MustParse("204m"),
			},
		}},
	}

	v1alpha1BlockedScaling := v1alpha1.BlockedScaling{
		Reason: v1alpha1.BlockingReasonMinChange,
		ScalingStatus: v1alpha1.ScalingStatus{
			HpaStatus: v1alpha1.HpaStatus{
				CurrentReplicas: 11,
				DesiredReplicas: 22,
			},
			VpaStatus: vpa_api.VerticalPodAutoscalerStatus{
				Recommendation: &vpaRecommendation,
			},
		},
	}

	v1alpha2BlockedScaling := v1alpha2.BlockedScaling{
		Reason: v1alpha2.BlockingReasonMinChange,
		ScalingStatus: v1alpha2.ScalingStatus{
			HpaStatus: v1alpha2.HpaStatus{
				CurrentReplicas: 11,
				DesiredReplicas: 22,
			},
			VpaStatus: vpa_api.VerticalPodAutoscalerStatus{
				Recommendation: &vpaRecommendation,
			},
		},
	}

	v1alpha1HVPA := v1alpha1.Hvpa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"hpa-controller": "hvpa",
			},
		},
		Spec: v1alpha1.HvpaSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       target,
				APIVersion: "apps/v1",
			},
			Hpa: v1alpha1.HpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"hpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: v1alpha1.ScaleType{
					UpdatePolicy: v1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},
				ScaleDown: v1alpha1.ScaleType{
					UpdatePolicy: v1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},

				Template: v1alpha1.HpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"hpaKey": labelVal,
						},
					},
					Spec: v1alpha1.HpaTemplateSpec{
						MinReplicas: &replica,
						MaxReplicas: 3,
						Metrics: []autoscaling.MetricSpec{
							{
								Type: autoscaling.ResourceMetricSourceType,
								Resource: &autoscaling.ResourceMetricSource{
									Name:                     v1.ResourceCPU,
									TargetAverageUtilization: &util,
								},
							},
						},
					},
				},
			},
			Vpa: v1alpha1.VpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: v1alpha1.ScaleType{
					UpdatePolicy: v1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange:             minChange,
				},
				ScaleDown: v1alpha1.ScaleType{
					UpdatePolicy: v1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange:             minChange,
				},
				Template: v1alpha1.VpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"vpaKey": labelVal,
						},
					},
				},
			},
			WeightBasedScalingIntervals: []v1alpha1.WeightBasedScalingInterval{
				{
					StartReplicaCount: 1,
					LastReplicaCount:  2,
					VpaWeight:         30,
				},
				{
					StartReplicaCount: 2,
					LastReplicaCount:  3,
					VpaWeight:         80,
				},
			},
			MaintenanceTimeWindow: &v1alpha1.MaintenanceTimeWindow{
				Begin: "maintenance-begin",
				End:   "maintenance-end",
			},
		},
		Status: v1alpha1.HvpaStatus{
			Replicas:       &statusReplicas,
			TargetSelector: &target,
			HpaScaleUpUpdatePolicy: &v1alpha1.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			HpaScaleDownUpdatePolicy: &v1alpha1.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			VpaScaleUpUpdatePolicy: &v1alpha1.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			VpaScaleDownUpdatePolicy: &v1alpha1.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			HpaWeight:                    33,
			VpaWeight:                    67,
			OverrideScaleUpStabilization: true,
			LastBlockedScaling:           []*v1alpha1.BlockedScaling{&v1alpha1BlockedScaling},
			LastScaling: v1alpha1.ScalingStatus{
				LastScaleTime: &t,
				HpaStatus: v1alpha1.HpaStatus{
					CurrentReplicas: 10,
					DesiredReplicas: 12,
				},
				VpaStatus: vpa_api.VerticalPodAutoscalerStatus{
					Recommendation: &vpaRecommendation,
					Conditions: []vpa_api.VerticalPodAutoscalerCondition{{
						Status:             "True",
						Type:               vpa_api.RecommendationProvided,
						LastTransitionTime: t,
						Reason:             "vpa-condition-reason",
						Message:            "vpa-condition-message",
					}},
				},
			},
			LastError: &v1alpha1.LastError{
				Description:    "last-error",
				LastUpdateTime: t,
				LastOperation:  "last-operation-before-error",
			},
		},
	}

	v1alpha2HVPA := v1alpha2.Hvpa{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"hpa-controller": "hvpa",
			},
		},
		Spec: v1alpha2.HvpaSpec{
			TargetRef: &autoscalingv2.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       target,
				APIVersion: "apps/v1",
			},
			Hpa: v1alpha2.HpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"hpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: v1alpha2.ScaleType{
					UpdatePolicy: v1alpha2.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},
				ScaleDown: v1alpha2.ScaleType{
					UpdatePolicy: v1alpha2.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},

				Template: v1alpha2.HpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"hpaKey": labelVal,
						},
					},
					Spec: v1alpha2.HpaTemplateSpec{
						MinReplicas: &replica,
						MaxReplicas: 3,
						Metrics: []autoscalingv2.MetricSpec{
							{
								Type: autoscalingv2.ResourceMetricSourceType,
								Resource: &autoscalingv2.ResourceMetricSource{
									Name: v1.ResourceCPU,
									Target: autoscalingv2.MetricTarget{
										Type:               autoscalingv2.UtilizationMetricType,
										AverageUtilization: &util,
									},
								},
							},
						},
					},
				},
			},
			Vpa: v1alpha2.VpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: v1alpha2.ScaleType{
					UpdatePolicy: v1alpha2.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange: v1alpha2.ScaleParams{
						CPU: v1alpha2.ChangeParams{
							Value:      &valCPU,
							Percentage: &percCPU,
						},
						Memory: v1alpha2.ChangeParams{
							Value:      &valMem,
							Percentage: &percMem,
						},
					},
				},
				ScaleDown: v1alpha2.ScaleType{
					UpdatePolicy: v1alpha2.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange: v1alpha2.ScaleParams{
						CPU: v1alpha2.ChangeParams{
							Value:      &valCPU,
							Percentage: &percCPU,
						},
						Memory: v1alpha2.ChangeParams{
							Value:      &valMem,
							Percentage: &percMem,
						},
					},
				},
				Template: v1alpha2.VpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"vpaKey": labelVal,
						},
					},
				},
			},
			WeightBasedScalingIntervals: []v1alpha2.WeightBasedScalingInterval{
				{
					StartReplicaCount: 1,
					LastReplicaCount:  2,
					VpaWeight:         30,
				},
				{
					StartReplicaCount: 2,
					LastReplicaCount:  3,
					VpaWeight:         80,
				},
			},
			MaintenanceTimeWindow: &v1alpha2.MaintenanceTimeWindow{
				Begin: "maintenance-begin",
				End:   "maintenance-end",
			},
		},
		Status: v1alpha2.HvpaStatus{
			Replicas:       &statusReplicas,
			TargetSelector: &target,
			HpaScaleUpUpdatePolicy: &v1alpha2.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			HpaScaleDownUpdatePolicy: &v1alpha2.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			VpaScaleUpUpdatePolicy: &v1alpha2.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			VpaScaleDownUpdatePolicy: &v1alpha2.UpdatePolicy{
				UpdateMode: &updateMode,
			},
			HpaWeight:                    33,
			VpaWeight:                    67,
			OverrideScaleUpStabilization: true,
			LastBlockedScaling:           []*v1alpha2.BlockedScaling{&v1alpha2BlockedScaling},
			LastScaling: v1alpha2.ScalingStatus{
				LastScaleTime: &t,
				HpaStatus: v1alpha2.HpaStatus{
					CurrentReplicas: 10,
					DesiredReplicas: 12,
				},
				VpaStatus: vpa_api.VerticalPodAutoscalerStatus{
					Recommendation: &vpaRecommendation,
					Conditions: []vpa_api.VerticalPodAutoscalerCondition{{
						Status:             "True",
						Type:               vpa_api.RecommendationProvided,
						LastTransitionTime: t,
						Reason:             "vpa-condition-reason",
						Message:            "vpa-condition-message",
					}},
				},
			},
			LastError: &v1alpha2.LastError{
				Description:    "last-error",
				LastUpdateTime: t,
				LastOperation:  "last-operation-before-error",
			},
		},
	}

	format.MaxLength = 0 // diffs between HVPA object can be quite large, so make sure we see what's different
	It("Should not error on empty conversion from v1alpha1 to v1alpha2", func() {
		destination := v1alpha2.Hvpa{}
		source := v1alpha1.Hvpa{}
		Expect(destination.ConvertFrom(&source)).To(Succeed())
		Expect(destination).To(Equal(v1alpha2.Hvpa{}))
	})

	It("Should not error on empty conversion from v1alpha2 to v1alpha1", func() {
		destination := v1alpha1.Hvpa{}
		source := v1alpha2.Hvpa{}
		Expect(source.ConvertTo(&destination)).To(Succeed())
		Expect(destination).To(Equal(v1alpha1.Hvpa{}))
	})

	It("Should convert from v1alpha1 to v1alpha2", func() {
		destination := v1alpha2.Hvpa{}
		Expect(destination.ConvertFrom(&v1alpha1HVPA)).To(Succeed())
		Expect(destination).To(Equal(v1alpha2HVPA))
	})

	It("Should convert from v1alpha2 to v1alpha1", func() {
		destination := v1alpha1.Hvpa{}
		Expect(v1alpha2HVPA.ConvertTo(&destination)).To(Succeed())
		Expect(destination).To(Equal(v1alpha1HVPA))
	})
})
