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

package controllers

import (
	"context"
	"fmt"
	"time"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	"github.com/gardener/hvpa-controller/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const timeout = time.Second * 5

var (
	limitEquallyScaled = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("340000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("340000k"),
			v1.ResourceCPU:    resource.MustParse("200m"),
		},
	}
	requestsCappedToLimit = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("600M"),
		},
		Requests: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("600M"),
			v1.ResourceCPU:    resource.MustParse("500m"),
		},
	}
	scaledByWeight40 = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("2376000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("300m"),
			v1.ResourceMemory: resource.MustParse("1320000k"),
		},
	}
	proportionalScaled = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("33000000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("300m"),
			v1.ResourceMemory: resource.MustParse("1320000k"),
		},
	}
	unscaled = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("5G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("300m"),
			v1.ResourceMemory: resource.MustParse("200M"),
		},
	}
	target  = newTarget("deployment", unscaled, 2)
	valMem  = "100M"
	valCPU  = "100m"
	percMem = int32(80)
	percCPU = int32(80)

	limValMem  = "1G"
	limValCPU  = "1"
	limPercMem = int32(80)
	limPercCPU = int32(80)

	minChange = autoscalingv1alpha1.ScaleParams{
		CPU: autoscalingv1alpha1.ChangeParams{
			Value:      &valCPU,
			Percentage: &percCPU,
		},
		Memory: autoscalingv1alpha1.ChangeParams{
			Value:      &valMem,
			Percentage: &percMem,
		},
	}

	limitScale = autoscalingv1alpha1.ScaleParams{
		CPU: autoscalingv1alpha1.ChangeParams{
			Value:      &limValCPU,
			Percentage: &limPercCPU,
		},
		Memory: autoscalingv1alpha1.ChangeParams{
			Value:      &limValMem,
			Percentage: &limPercMem,
		},
	}
)

var _ = Describe("#TestReconcile", func() {

	DescribeTable("##ReconcileHPAandVPA",
		func(instance *autoscalingv1alpha1.Hvpa) {
			deploytest := target.DeepCopy()
			// Overwrite name
			deploytest.Name = "deploy-test-1"

			c := mgr.GetClient()
			// Create the test deployment
			err := c.Create(context.TODO(), deploytest)
			Expect(err).NotTo(HaveOccurred())

			// Create the Hvpa object and expect the Reconcile and HPA to be created
			err = c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			defer c.Delete(context.TODO(), instance)

			hpaList := &autoscaling.HorizontalPodAutoscalerList{}
			hpa := &autoscaling.HorizontalPodAutoscaler{}
			Eventually(func() error {
				num := 0
				c.List(context.TODO(), hpaList)
				for _, obj := range hpaList.Items {
					if obj.GenerateName == "hvpa-1-" {
						num = num + 1
						hpa = obj.DeepCopy()
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Expected 1 HPA; found %v", len(hpaList.Items))
			}, timeout).Should(Succeed())

			vpaList := &vpa_api.VerticalPodAutoscalerList{}
			vpa := &vpa_api.VerticalPodAutoscaler{}
			Eventually(func() error {
				num := 0
				c.List(context.TODO(), vpaList)
				for _, obj := range vpaList.Items {
					if obj.GenerateName == "hvpa-1-" {
						num = num + 1
						vpa = obj.DeepCopy()
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Expected 1 VPA; found %v", len(vpaList.Items))
			}, timeout).Should(Succeed())

			// Delete the HPA and expect Reconcile to be called for HPA deletion
			Expect(c.Delete(context.TODO(), hpa)).NotTo(HaveOccurred())
			Eventually(func() error {
				num := 0
				c.List(context.TODO(), hpaList)
				for _, obj := range hpaList.Items {
					if obj.GenerateName == "hvpa-1-" {
						num = num + 1
						hpa = obj.DeepCopy()
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Expected 1 HPA; found %v", len(hpaList.Items))
			}, timeout).Should(Succeed())

			// Create a pod for the target deployment, and update status to "OOMKilled".
			// The field hvpa.status.overrideScaleUpStabilization should be set to true.
			p := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"name": target.Name,
					},
				},
				Spec: v1.PodSpec{
					NodeName: "test-node",
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: "k8s.gcr.io/pause-amd64:3.0",
						},
					},
				},
			}
			Expect(c.Create(context.TODO(), &p)).To(Succeed())
			p.Status = v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						RestartCount: 2,
						LastTerminationState: v1.ContainerState{
							Terminated: &v1.ContainerStateTerminated{
								Reason:     "OOMKilled",
								FinishedAt: metav1.Now(),
							},
						},
					},
				},
			}
			Expect(c.Status().Update(context.TODO(), &p)).To(Succeed())

			Eventually(func() bool {
				h := &autoscalingv1alpha1.Hvpa{}
				c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, h)
				return h.Status.OverrideScaleUpStabilization
			}, timeout).Should(BeTrue())

			// Manually delete HPA & VPA since GC isn't enabled in the test control plane
			Eventually(func() error { return c.Delete(context.TODO(), hpa) }, timeout).
				Should(MatchError(fmt.Sprintf("horizontalpodautoscalers.autoscaling \"%s\" not found", hpa.Name)))
			Eventually(func() error { return c.Delete(context.TODO(), vpa) }, timeout).
				Should(MatchError(fmt.Sprintf("verticalpodautoscalers.autoscaling.k8s.io \"%s\" not found", vpa.Name)))

			// Delete the test deployment
			Expect(c.Delete(context.TODO(), deploytest)).NotTo(HaveOccurred())
		},
		Entry("hvpa", newHvpa("hvpa-1", "deploy-test-1", "label-1", minChange)),
	)

	Describe("#ScaleTests", func() {
		type setup struct {
			hvpa      *autoscalingv1alpha1.Hvpa
			hpaStatus *autoscaling.HorizontalPodAutoscalerStatus
			vpaStatus *vpa_api.VerticalPodAutoscalerStatus
			target    *appsv1.Deployment
			vpaWeight int32
		}
		type expect struct {
			scalingOff      bool
			desiredReplicas int32
			resourceChange  bool
			scaleOutLimited bool
			resources       v1.ResourceRequirements
			blockedReasons  []autoscalingv1alpha1.BlockingReason
		}
		type action struct {
			maintenanceWindow    *autoscalingv1alpha1.MaintenanceTimeWindow
			updateMode           string
			limitScaling         autoscalingv1alpha1.ScaleParams
			vpaStatusCondition   []vpa_api.VerticalPodAutoscalerCondition
			vpaPodResourcePolicy *vpa_api.PodResourcePolicy
		}
		type data struct {
			setup  setup
			action action
			expect expect
		}
		requestsOnly := vpa_api.ContainerControlledValuesRequestsOnly

		DescribeTable("##ScaleTestScenarios",
			func(data *data) {
				hvpa := data.setup.hvpa
				hpaStatus := data.setup.hpaStatus
				vpaStatus := data.setup.vpaStatus
				target := data.setup.target
				vpaWeight := data.setup.vpaWeight

				hvpa.Spec.Vpa.LimitsRequestsGapScaleParams = data.action.limitScaling
				if data.action.maintenanceWindow != nil {
					hvpa.Spec.MaintenanceTimeWindow = data.action.maintenanceWindow
				}
				if data.action.updateMode != "" {
					hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Hpa.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Vpa.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Vpa.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
				}
				if data.action.vpaPodResourcePolicy != nil {
					hvpa.Spec.Vpa.Template.Spec.ResourcePolicy = data.action.vpaPodResourcePolicy
				}

				if data.action.vpaStatusCondition != nil {
					vpaStatus.Conditions = append(data.action.vpaStatusCondition, vpaStatus.Conditions...)
				}

				scaleOutLimited := isHpaScaleOutLimited(
					data.setup.hpaStatus,
					data.setup.hvpa.Spec.Hpa.Template.Spec.MaxReplicas,
					hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode,
					hvpa.Spec.MaintenanceTimeWindow,
				)

				Expect(isScalingOff(data.setup.hvpa)).To(Equal(data.expect.scalingOff))
				Expect(scaleOutLimited).To(Equal(data.expect.scaleOutLimited))

				blockedScaling := &[]*autoscalingv1alpha1.BlockedScaling{}
				podSpec, resourceChanged, vpaStatus, err := getWeightedRequests(vpaStatus, hvpa, vpaWeight, &target.Spec.Template.Spec, scaleOutLimited, blockedScaling)

				Expect(err).ToNot(HaveOccurred())
				Expect(resourceChanged).To(Equal(data.expect.resourceChange))
				Expect(len(*blockedScaling)).To(Equal(len(data.expect.blockedReasons)))

				if len(data.expect.blockedReasons) != 0 {
					Expect(len(*blockedScaling)).To(Equal(len(data.expect.blockedReasons)))
					for i, blockedScaling := range *blockedScaling {
						Expect(blockedScaling.Reason).To(Equal(data.expect.blockedReasons[i]))
					}
				}

				if data.expect.resourceChange {
					Expect(podSpec).NotTo(BeNil())
					if podMemLimits := podSpec.Containers[0].Resources.Limits.Memory(); !podMemLimits.IsZero() {
						Expect(podMemLimits.Cmp(*podSpec.Containers[0].Resources.Requests.Memory())).NotTo(Equal(-1))
					}
					if podCPULimits := podSpec.Containers[0].Resources.Limits.Cpu(); !podCPULimits.IsZero() {
						Expect(podCPULimits.Cmp(*podSpec.Containers[0].Resources.Requests.Cpu())).NotTo(Equal(-1))
					}
					Expect(podSpec.Containers[0].Resources).To(Equal(data.expect.resources))
				} else {
					Expect(podSpec).To(BeNil())
				}

				hpaStatus, err = getWeightedReplicas(hpaStatus, hvpa, *target.Spec.Replicas, 100-vpaWeight, blockedScaling)
				Expect(err).ToNot(HaveOccurred())
				Expect(hpaStatus.DesiredReplicas).To(Equal(data.expect.desiredReplicas))
			},

			Entry("UpdateMode Auto, Should Scale only memory", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
					vpaWeight: int32(40),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       scaledByWeight40,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, blocked Scaling", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "250M", "350m"),
					target:    target,
					vpaWeight: int32(90),
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  false,
					scaleOutLimited: true,
					resources:       unscaled,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, blocked scaling", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 1, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "50M", "50m"),
					target:    target,
					vpaWeight: int32(90),
				},
				action: action{
					maintenanceWindow: &autoscalingv1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()+3)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+4)%24, 0, 0).Formatted(),
					},
					updateMode: autoscalingv1alpha1.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					scalingOff:      true,
					desiredReplicas: 2,
					resourceChange:  false,
					scaleOutLimited: true, // Scale out is limited due to HPA update mode: MaintenanceMode
					resources:       unscaled,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMaintenanceWindow,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, Should Scale only memory", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "200m"),
					target:    target,
					vpaWeight: int32(40),
				},
				action: action{
					maintenanceWindow: &autoscalingv1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime(time.Now().UTC().Hour()-1, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime(time.Now().UTC().Hour()+1, 0, 0).Formatted(),
					},
					updateMode:   autoscalingv1alpha1.UpdateModeMaintenanceWindow,
					limitScaling: limitScale,
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       scaledByWeight40,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, HPA scaling not limited", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
					vpaWeight: int32(40),
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  false,
					scaleOutLimited: false,
					resources:       unscaled,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonUpdatePolicy,
					},
				},
			}),
			Entry("UpdateMode Auto, Proportional scaling", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
					vpaWeight: int32(40),
				},
				action: action{
					limitScaling: autoscalingv1alpha1.ScaleParams{},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       proportionalScaled,
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, Requests set equal to limits", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "100M", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("500M"),
							},
							Requests: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("500M"),
							},
						},
						2),
					vpaWeight: int32(40),
				},
				action: action{
					limitScaling: autoscalingv1alpha1.ScaleParams{},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       limitEquallyScaled,
					blockedReasons:  []autoscalingv1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, ControlledValues RequestsOnly, Request higher than fixed limit", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						3, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "800M", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("600M"),
							},
							Requests: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse("500M"),
							},
						},
						3),
					vpaWeight: int32(100),
				},
				action: action{
					limitScaling:         autoscalingv1alpha1.ScaleParams{},
					vpaPodResourcePolicy: &vpa_api.PodResourcePolicy{ContainerPolicies: []vpa_api.ContainerResourcePolicy{{ContainerName: "deployment", ControlledValues: &requestsOnly}}},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       requestsCappedToLimit,
					blockedReasons:  []autoscalingv1alpha1.BlockingReason{},
				},
			}),
			Entry("VPA unsupported condition", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						2, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionTrue,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
					vpaWeight: int32(40),
				},
				action: action{
					vpaStatusCondition: []vpa_api.VerticalPodAutoscalerCondition{
						{
							Type:   vpa_api.ConfigUnsupported,
							Status: v1.ConditionTrue,
						},
					},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  false,
					scaleOutLimited: true,
					resources:       unscaled,
					blockedReasons:  []autoscalingv1alpha1.BlockingReason{},
				},
			}),
		)
	})
})
