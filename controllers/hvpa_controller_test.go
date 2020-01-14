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

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/gardener/hvpa-controller/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"time"
)

const timeout = time.Second * 5
const testNamespace = "test"

var (
	valMem  = "100M"
	valCPU  = "100m"
	percMem = int32(80)
	percCPU = int32(80)

	limValMem  = "3G"
	limValCPU  = "2"
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

			replica := int32(1)
			deploytest := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deploy-test-1",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replica,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"name": "testDeployment",
						},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"name": "testDeployment",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								v1.Container{
									Name:  "pause",
									Image: "k8s.gcr.io/pause-amd64:3.0",
								},
							},
						},
					},
				},
			}

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

			// Manually delete HPA & VPA since GC isn't enabled in the test control plane
			Eventually(func() error { return c.Delete(context.TODO(), hpa) }, timeout).
				Should(MatchError(fmt.Sprintf("horizontalpodautoscalers.autoscaling \"%s\" not found", hpa.Name)))
			Eventually(func() error { return c.Delete(context.TODO(), vpa) }, timeout).
				Should(MatchError(fmt.Sprintf("verticalpodautoscalers.autoscaling.k8s.io \"%s\" not found", vpa.Name)))

			// Delete the test deployment
			Expect(c.Delete(context.TODO(), deploytest)).NotTo(HaveOccurred())
		},
		Entry("hvpa", newHvpa("hvpa-1", "deploy-test-1", "label-1", minChange, limitScale)),
	)

	Describe("#ScaleTests", func() {
		type setup struct {
			hvpa      *autoscalingv1alpha1.Hvpa
			hpaStatus *autoscaling.HorizontalPodAutoscalerStatus
			vpaStatus *vpa_api.VerticalPodAutoscalerStatus
			target    *appsv1.Deployment
			vpaWeight autoscalingv1alpha1.VpaWeight
		}
		type expect struct {
			desiredReplicas int32
			resourceChange  bool
			resources       v1.ResourceRequirements
			blockedReasons  []autoscalingv1alpha1.BlockingReason
		}
		type action struct {
			maintenanceWindow *autoscalingv1alpha1.MaintenanceTimeWindow
			updateMode        string
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

				if data.action.maintenanceWindow != nil {
					hvpa.Spec.MaintenanceTimeWindow = data.action.maintenanceWindow
				}
				if data.action.updateMode != "" {
					hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Hpa.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Vpa.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.Vpa.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
				}

				blockedScaling := &[]*autoscalingv1alpha1.BlockedScaling{}
				podSpec, resourceChanged, vpaStatus, err := getWeightedRequests(vpaStatus, hvpa, vpaWeight, &target.Spec.Template.Spec, true, blockedScaling)

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

				hpaStatus, err = getWeightedReplicas(hpaStatus, hvpa, *target.Spec.Replicas, 100-vpaWeight, blockedScaling)
				Expect(err).ToNot(HaveOccurred())
				Expect(hpaStatus.DesiredReplicas).To(Equal(data.expect.desiredReplicas))
			},

			Entry("UpdateMode Auto, Should Scale only memory", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", "deployment", "label-2", minChange, limitScale),
					hpaStatus: newHpaStatus(2, 3),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", "2", "5G", "300m", "200M", 2),
					vpaWeight: autoscalingv1alpha1.VpaWeight(40),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2376000k"),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("300m"),
							v1.ResourceMemory: resource.MustParse("1320000000"),
						},
					},
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode Auto, blocked Scaling", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", "deployment", "label-2", minChange, limitScale),
					hpaStatus: newHpaStatus(2, 3),
					vpaStatus: newVpaStatus("deployment", "250M", "350m"),
					target:    newTarget("deployment", "2", "5Gi", "300m", "200M", 2),
					vpaWeight: autoscalingv1alpha1.VpaWeight(90),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  false,
					resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("5Gi"),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("300m"),
							v1.ResourceMemory: resource.MustParse("200M"),
						},
					},
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, blocked scaling", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", "deployment", "label-2", minChange, limitScale),
					hpaStatus: newHpaStatus(2, 3),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", "2", "5Gi", "300m", "200M", 2),
					vpaWeight: autoscalingv1alpha1.VpaWeight(90),
				},
				action: action{
					maintenanceWindow: &autoscalingv1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime(time.Now().UTC().Hour()+3, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime(time.Now().UTC().Hour()+4, 0, 0).Formatted(),
					},
					updateMode: autoscalingv1alpha1.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  false,
					resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("5Gi"),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("300m"),
							v1.ResourceMemory: resource.MustParse("200M"),
						},
					},
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMaintenanceWindow,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, Should Scale only memory", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", "deployment", "label-2", minChange, limitScale),
					hpaStatus: newHpaStatus(2, 3),
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    newTarget("deployment", "2", "5G", "300m", "200M", 2),
					vpaWeight: autoscalingv1alpha1.VpaWeight(40),
				},
				action: action{
					maintenanceWindow: &autoscalingv1alpha1.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime(time.Now().UTC().Hour()-1, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime(time.Now().UTC().Hour()+1, 0, 0).Formatted(),
					},
					updateMode: autoscalingv1alpha1.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("2376000k"),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("300m"),
							v1.ResourceMemory: resource.MustParse("1320000000"),
						},
					},
					blockedReasons: []autoscalingv1alpha1.BlockingReason{
						autoscalingv1alpha1.BlockingReasonMinChange,
					},
				},
			}),
		)
	})
})
