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
	scaledSmall = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("270m"),
			v1.ResourceMemory: resource.MustParse("3960000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("2200000k"),
		},
	}
	unscaledSmall = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("3"),
			v1.ResourceMemory: resource.MustParse("4G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("1.8G"),
		},
	}
	scaledLarge = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1225m"),
			v1.ResourceMemory: resource.MustParse("2160000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("225m"),
			v1.ResourceMemory: resource.MustParse("1200000k"),
		},
	}
	unscaledLarge = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("3"),
			v1.ResourceMemory: resource.MustParse("4G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("2.2G"),
		},
	}
	target = newTarget("deployment", unscaled, 2)

	minChange = hvpav1alpha1.ScaleParams{
		CPU: hvpav1alpha1.ChangeParams{
			Value:      stringPtr("100m"),
			Percentage: int32Ptr(80),
		},
		Memory: hvpav1alpha1.ChangeParams{
			Value:      stringPtr("100M"),
			Percentage: int32Ptr(80),
		},
	}

	limitScale = hvpav1alpha1.ScaleParams{
		CPU: hvpav1alpha1.ChangeParams{
			Value:      stringPtr("1"),
			Percentage: int32Ptr(80),
		},
		Memory: hvpav1alpha1.ChangeParams{
			Value:      stringPtr("1"),
			Percentage: int32Ptr(80),
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
		}
		type expect struct {
			desiredReplicas int32
			resourceChange  bool
			resources       v1.ResourceRequirements
			blockedReasons  []hvpav1alpha1.BlockingReason
		}
		type action struct {
			maintenanceWindow       *hvpav1alpha1.MaintenanceTimeWindow
			updateMode              string
			limitScaling            hvpav1alpha1.ScaleParams
			scaleIntervals          []hvpav1alpha1.ScaleIntervals
			vpaStatusCondition      []vpa_api.VerticalPodAutoscalerCondition
			baseResourcesPerReplica hvpav1alpha1.ResourceChangeParams
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

				hvpa.Spec.Vpa.LimitsRequestsGapScaleParams = data.action.limitScaling
				if data.action.maintenanceWindow != nil {
					hvpa.Spec.MaintenanceTimeWindow = data.action.maintenanceWindow
				}
				if data.action.updateMode != "" {
					hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
				}
				if data.action.vpaStatusCondition != nil {
					vpaStatus.Conditions = append(data.action.vpaStatusCondition, vpaStatus.Conditions...)
				}
				if data.action.scaleIntervals != nil {
					hvpa.Spec.ScaleIntervals = data.action.scaleIntervals
				}
				if data.action.baseResourcesPerReplica != nil {
					hvpa.Spec.BaseResourcesPerReplica = data.action.baseResourcesPerReplica
				}
				if data.action.vpaStatusCondition != nil {
					vpaStatus.Conditions = append(data.action.vpaStatusCondition, vpaStatus.Conditions...)
				}

				scaledStatus, newPodSpec, resourceChanged, blockedScaling, err := getScalingRecommendations(hpaStatus, vpaStatus, hvpa, &target.Spec.Template.Spec, *target.Spec.Replicas)

				if data.action.vpaStatusCondition != nil {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(resourceChanged).To(Equal(data.expect.resourceChange))

				Expect(len(*blockedScaling)).To(Equal(len(data.expect.blockedReasons)))
				if len(data.expect.blockedReasons) != 0 {
					for i, blockedScaling := range *blockedScaling {
						Expect(blockedScaling.Reason).To(Equal(data.expect.blockedReasons[i]))
					}
				}

				if data.expect.desiredReplicas == *target.Spec.Replicas && data.expect.resourceChange == false {
					Expect(scaledStatus).To(BeNil())
				} else {
					Expect(scaledStatus.HpaStatus.DesiredReplicas).To(Equal(data.expect.desiredReplicas))
				}
				if data.expect.resourceChange {
					Expect(newPodSpec).NotTo(BeNil())
					Expect(newPodSpec.Containers[0].Resources).To(Equal(data.expect.resources))
				} else {
					Expect(newPodSpec).To(BeNil())
				}
			},

			Entry("UpdateMode Auto, scale up, paradoxical scaling, replicas increases, resources per replica decreases", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "2.2G", "150m"),
					target:    newTarget("deployment", unscaledSmall, 1),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  false,
					resources:       unscaledSmall,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scaled down, no scaling because of paradoxical scaling recommendations", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonParadoxicalScaling,
					},
				},
			}),
			Entry("UpdateMode Auto, overall scale up, paradoxical scaling, but replicas decrease - should not be considered paradoxical", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 3),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("750m"),
							"memory": resource.MustParse("6000000k"),
						},
					},
					blockedReasons: []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, overall scale down, paradoxical scaling, but replicas increase - should not be considered paradoxical", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "1500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("15"),
								v1.ResourceMemory: resource.MustParse("20G"),
							},
						}, 1),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("750m"),
							"memory": resource.MustParse("2000000k"),
						},
					},
					blockedReasons: []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale up blocked due to minChange", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.89G", "150m"),
					target:    newTarget("deployment", unscaledSmall, 1),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 1,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, blocked scaling", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "0.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()+3)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+4)%24, 0, 0).Formatted(),
					},
					updateMode: hvpav1alpha1.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha1.BlockingReason{
						hvpav1alpha1.BlockingReasonMaintenanceWindow,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, scale down", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "0.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()-1)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+1)%24, 0, 0).Formatted(),
					},
					updateMode:   hvpav1alpha1.UpdateModeMaintenanceWindow,
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources:       scaledLarge,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("VPA unsupported condition", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
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
					resourceChange: false,
					blockedReasons: []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale down hysteresis", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "5.486G", "2.828"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"cpu":    resource.MustParse("8"),
								"memory": resource.MustParse("10G"),
							},
						}, 3),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					blockedReasons:  []hvpav1alpha1.BlockingReason{},
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("2828m"),
							"memory": resource.MustParse("5486000k"),
						},
					},
				},
			}),
			Entry("UpdateMode Auto, scale up, base resource usage adjusted", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 1),
				},
				action: action{
					baseResourcesPerReplica: hvpav1alpha1.ResourceChangeParams{
						"cpu": hvpav1alpha1.ChangeParams{
							Value: stringPtr("100m"),
						},
						"memory": hvpav1alpha1.ChangeParams{
							Percentage: int32Ptr(10),
						},
					},
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("300m"),
							"memory": resource.MustParse("2110000k"),
						},
					},
					blockedReasons: []hvpav1alpha1.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale up, nil base resource usage", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 1),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("250m"),
							"memory": resource.MustParse("2000000k"),
						},
					},
					blockedReasons: []hvpav1alpha1.BlockingReason{},
				},
			}),
		)
	})
})
