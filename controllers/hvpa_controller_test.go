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

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
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
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
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
	scaledByWeight100 = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("3333m"),
			v1.ResourceMemory: resource.MustParse("75000000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("500m"),
			v1.ResourceMemory: resource.MustParse("3000000k"),
		},
	}
	target  = newTarget("deployment", unscaled, 2)
	valMem  = "100M"
	valCPU  = "100m"
	percMem = int32(80)
	percCPU = int32(80)

	limValMem  = "3G"
	limValCPU  = "2"
	limPercMem = int32(80)
	limPercCPU = int32(80)

	minChange = hvpav1alpha1.ScaleParams{
		CPU: hvpav1alpha1.ChangeParams{
			Value:      &valCPU,
			Percentage: &percCPU,
		},
		Memory: hvpav1alpha1.ChangeParams{
			Value:      &valMem,
			Percentage: &percMem,
		},
	}

	limitScale = hvpav1alpha1.ScaleParams{
		CPU: hvpav1alpha1.ChangeParams{
			Value:      &limValCPU,
			Percentage: &limPercCPU,
		},
		Memory: hvpav1alpha1.ChangeParams{
			Value:      &limValMem,
			Percentage: &limPercMem,
		},
	}
)

var _ = Describe("#TestReconcile", func() {

	DescribeTable("##ReconcileHPAandVPA",
		func(instance *hvpav1alpha1.Hvpa) {
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
				oldHpa := hpa.Name
				num := 0
				c.List(context.TODO(), hpaList)
				for _, obj := range hpaList.Items {
					if obj.GenerateName == "hvpa-1-" {
						num = num + 1
						hpa = obj.DeepCopy()
					}
				}
				if num == 1 && hpa.Name != oldHpa {
					return nil
				}
				return fmt.Errorf("Error: Expected 1 new HPA; found %v", len(hpaList.Items))
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
							Name:  "deploy-test-1",
							Image: "k8s.gcr.io/pause-amd64:3.0",
						},
					},
				},
				Status: v1.PodStatus{
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
				},
			}
			Expect(c.Create(context.TODO(), &p)).To(Succeed())
			Expect(c.Status().Update(context.TODO(), &p)).To(Succeed())

			Eventually(func() bool {
				h := &hvpav1alpha1.Hvpa{}
				c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, h)
				return h.Status.OverrideScaleUpStabilization
			}, timeout).Should(BeTrue())

			// Update VPA status, let HVPA scale
			hvpa := &hvpav1alpha1.Hvpa{}
			Expect(c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa)).To(Succeed())
			Expect(hvpa.Status.LastScaling.LastUpdated).To(BeNil())
			Eventually(func() error {
				if err := c.Get(context.TODO(), types.NamespacedName{Name: vpa.Name, Namespace: vpa.Namespace}, vpa); err != nil {
					return err
				}
				vpa.Status = *newVpaStatus("deployment", "3G", "500m")
				return c.Update(context.TODO(), vpa)
			}, timeout).Should(Succeed())

			Eventually(func() error {
				hvpa = &hvpav1alpha1.Hvpa{}
				if err := c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
					return err
				}
				if hvpa.Status.LastScaling.LastUpdated == nil {
					return fmt.Errorf("HVPA did not scale")
				}
				return nil
			}, timeout).Should(Succeed())

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
			hvpa      *hvpav1alpha1.Hvpa
			hpaStatus *autoscaling.HorizontalPodAutoscalerStatus
			vpaStatus *vpa_api.VerticalPodAutoscalerStatus
			target    *appsv1.Deployment
			vpaWeight hvpav1alpha1.VpaWeight
		}
		type expect struct {
			vpaWeight       hvpav1alpha1.VpaWeight
			scalingOff      bool
			desiredReplicas int32
			resourceChange  bool
			scaleOutLimited bool
			resources       v1.ResourceRequirements
			blockedReasons  []hvpav1alpha1.BlockingReason
		}
		type action struct {
			maintenanceWindow *hvpav1alpha1.MaintenanceTimeWindow
			updateMode        string
			limitScaling      hvpav1alpha1.ScaleParams
			weightIntervals   []hvpav1alpha1.WeightBasedScalingInterval
		}
		type data struct {
			setup  setup
			action action
			expect expect
		}

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
				if data.action.weightIntervals != nil {
					hvpa.Spec.WeightBasedScalingIntervals = data.action.weightIntervals
					vpaWeight = getVpaWeightFromIntervals(hvpa, hpaStatus.CurrentReplicas, vpaStatus)
					Expect(vpaWeight).To(Equal(data.expect.vpaWeight))
				}

				scaleOutLimited := isHpaScaleOutLimited(
					data.setup.hpaStatus,
					hvpav1alpha1.MaxWeight-vpaWeight,
					data.setup.hvpa.Spec.Hpa.Template.Spec.MaxReplicas,
					hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode,
					hvpa.Spec.MaintenanceTimeWindow,
				)

				Expect(isScalingOff(hvpa)).To(Equal(data.expect.scalingOff))
				Expect(scaleOutLimited).To(Equal(data.expect.scaleOutLimited))

				blockedScaling := &[]*hvpav1alpha1.BlockedScaling{}
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
					Expect(podSpec.Containers[0].Resources).To(Equal(data.expect.resources))
				} else {
					Expect(podSpec).To(BeNil())
				}

				// HPA would have set hvpa.spec.replicas to hpa.status.desiredReplicas using scale sub-resource
				hvpa.Spec.Replicas = &hpaStatus.DesiredReplicas
				hpaStatus, err = getWeightedReplicas(hpaStatus, hvpa, *target.Spec.Replicas, hvpav1alpha1.MaxWeight-vpaWeight, blockedScaling)
				Expect(err).ToNot(HaveOccurred())
				if data.expect.desiredReplicas != 0 {
					Expect(hpaStatus.DesiredReplicas).To(Equal(data.expect.desiredReplicas))
				} else {
					Expect(hpaStatus).To(BeNil())
				}
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
					vpaWeight: hvpav1alpha1.VpaWeight(40),
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
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
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
					vpaWeight: hvpav1alpha1.VpaWeight(90),
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  false,
					scaleOutLimited: true,
					resources:       unscaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
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
					vpaWeight: hvpav1alpha1.VpaWeight(90),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()+3)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+4)%24, 0, 0).Formatted(),
					},
					updateMode: hvpav1alpha1.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					scalingOff:      true,
					desiredReplicas: 2,
					resourceChange:  false,
					scaleOutLimited: true, // Scale out is limited due to HPA update mode: MaintenanceMode
					resources:       unscaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMaintenanceWindow,
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
					vpaWeight: hvpav1alpha1.VpaWeight(40),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime(time.Now().UTC().Hour()-1, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime(time.Now().UTC().Hour()+1, 0, 0).Formatted(),
					},
					updateMode:   hvpav1alpha1.UpdateModeMaintenanceWindow,
					limitScaling: limitScale,
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       scaledByWeight40,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
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
					vpaWeight: hvpav1alpha1.VpaWeight(40),
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: false,
					resources:       proportionalScaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
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
					vpaWeight: hvpav1alpha1.VpaWeight(40),
				},
				action: action{
					limitScaling: hvpav1alpha1.ScaleParams{},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       proportionalScaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
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
					vpaWeight: hvpav1alpha1.VpaWeight(40),
				},
				action: action{
					limitScaling: hvpav1alpha1.ScaleParams{},
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       limitEquallyScaled,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("Update Mode Auto, Target Replicas 0. Should not scale horizontally", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						0, 3, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", unscaled, 0),
					vpaWeight: hvpav1alpha1.VpaWeight(40),
				},
				expect: expect{
					scalingOff:      false,
					desiredReplicas: 0,
					resourceChange:  true,
					scaleOutLimited: false,
					resources:       proportionalScaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, Initial VPA scaling", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						1, 2, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", unscaled, 1),
				},
				action: action{
					weightIntervals: []hvpav1alpha1.WeightBasedScalingInterval{
						{
							StartReplicaCount:                       1,
							LastReplicaCount:                        1,
							VpaWeight:                               100,
							UpTransitionResourceThresholdPercentage: int64Ptr(90),
						},
						{
							StartReplicaCount: 2,
							LastReplicaCount:  4,
							VpaWeight:         40,
						},
					},
				},
				expect: expect{
					vpaWeight:       hvpav1alpha1.VpaWeight(100),
					scalingOff:      false,
					desiredReplicas: 1,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       scaledByWeight100,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, Initial VPA scaling, upper threshold crossed", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						1, 2, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", unscaled, 1),
				},
				action: action{
					weightIntervals: []hvpav1alpha1.WeightBasedScalingInterval{
						{
							StartReplicaCount:                       1,
							LastReplicaCount:                        1,
							VpaWeight:                               100,
							UpTransitionResourceThresholdPercentage: int64Ptr(30),
						},
						{
							StartReplicaCount: 2,
							LastReplicaCount:  4,
							VpaWeight:         40,
						},
					},
					limitScaling: limitScale,
				},
				expect: expect{
					vpaWeight:       hvpav1alpha1.VpaWeight(40),
					scalingOff:      false,
					desiredReplicas: 2,
					resourceChange:  true,
					scaleOutLimited: false,
					resources:       scaledByWeight40,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, Initial HPA scaling", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						1, 2, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", unscaled, 1),
				},
				action: action{
					weightIntervals: []hvpav1alpha1.WeightBasedScalingInterval{
						{
							StartReplicaCount: 1,
							LastReplicaCount:  1,
							VpaWeight:         0,
						},
						{
							StartReplicaCount: 2,
							LastReplicaCount:  2,
							VpaWeight:         100,
						},
						{
							StartReplicaCount: 3,
							LastReplicaCount:  4,
							VpaWeight:         0,
						},
					},
				},
				expect: expect{
					vpaWeight:       hvpav1alpha1.VpaWeight(0),
					scalingOff:      false,
					desiredReplicas: 2,
					resourceChange:  false,
					scaleOutLimited: false,
					resources:       unscaled,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonWeight,
					},
				},
			}),
			Entry("UpdateMode Auto, Middle VPA scaling", &data{
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
					target:    newTarget("deployment", unscaled, 2),
				},
				action: action{
					weightIntervals: []hvpav1alpha1.WeightBasedScalingInterval{
						{
							StartReplicaCount: 1,
							LastReplicaCount:  1,
							VpaWeight:         40,
						},
						{
							StartReplicaCount: 2,
							LastReplicaCount:  2,
							VpaWeight:         100,
							DownTransitionResourceThresholdPercentage: int64Ptr(70),
							UpTransitionResourceThresholdPercentage:   int64Ptr(90),
						},
						{
							StartReplicaCount: 3,
							LastReplicaCount:  4,
							VpaWeight:         0,
						},
					},
					limitScaling: limitScale,
				},
				expect: expect{
					vpaWeight:       hvpav1alpha1.VpaWeight(40),
					scalingOff:      false,
					desiredReplicas: 3,
					resourceChange:  true,
					scaleOutLimited: false,
					resources:       scaledByWeight40,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, VPA recommendation does not belong to any interval", &data{
				setup: setup{
					hvpa: newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: newHpaStatus(
						1, 2, []autoscaling.HorizontalPodAutoscalerCondition{
							{
								Type:   autoscaling.ScalingLimited,
								Status: v1.ConditionFalse,
							},
						}),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", unscaled, 1),
				},
				action: action{
					weightIntervals: []hvpav1alpha1.WeightBasedScalingInterval{
						{
							StartReplicaCount:                       1,
							LastReplicaCount:                        1,
							VpaWeight:                               100,
							UpTransitionResourceThresholdPercentage: int64Ptr(30),
						},
						{
							StartReplicaCount: 2,
							LastReplicaCount:  2,
							VpaWeight:         100,
							DownTransitionResourceThresholdPercentage: int64Ptr(70),
						},
					},
				},
				expect: expect{
					vpaWeight:       hvpav1alpha1.VpaWeight(100),
					scalingOff:      false,
					desiredReplicas: 1,
					resourceChange:  true,
					scaleOutLimited: true,
					resources:       scaledByWeight100,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
				},
			}),
		)
	})
})
