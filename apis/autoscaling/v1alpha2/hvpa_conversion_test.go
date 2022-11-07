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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("HvpaConversion", func() {
	format.MaxLength = 0
	It("Should not error on empty conversion from v1alpha1 to v1alpha2", func() {
		destination := v1alpha2.Hvpa{}
		source := v1alpha1.Hvpa{}
		Expect(destination.ConvertFrom(&source)).To(Succeed())
		Expect(destination).To(Equal(v1alpha2.Hvpa{}))
	})

	It("Should convert from v1alpha1 to v1alpha2", func() {
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

		source := v1alpha1.Hvpa{
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
		}
		destination := v1alpha2.Hvpa{}
		Expect(destination.ConvertFrom(&source)).To(Succeed())
		Expect(destination).To(Equal(v1alpha2.Hvpa{
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
			Status: v1alpha2.HvpaStatus{},
		}))
	})

	It("Should convert from v1alpha2 to v1alpha1", func() {
		src := v1alpha2.Hvpa{}
		destination := v1alpha1.Hvpa{}
		Expect(src.ConvertTo(&destination)).To(Succeed())
		Expect(destination).To(Equal(v1alpha1.Hvpa{}))
	})
})
