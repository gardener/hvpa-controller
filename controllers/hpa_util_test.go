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
	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("getHpaFromHvpa", func() {
	var (
		hvpa        *hvpav1alpha1.Hvpa
		expectedHpa *autoscaling.HorizontalPodAutoscaler
		matchErr    gomegatypes.GomegaMatcher
	)

	BeforeEach(func() {
		hvpa = &hvpav1alpha1.Hvpa{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "instance",
				Namespace: "default",
			},
		}

		expectedHpa = nil
		matchErr = Succeed()
	})

	JustBeforeEach(func() {
		var hpa, err = getHpaFromHvpa(hvpa)

		Expect(err).To(matchErr)
		if expectedHpa == nil {
			Expect(hpa).To(BeNil())
		} else {
			Expect(hpa).To(Equal(expectedHpa))
		}
	})

	Describe("when HPA template has ownerReferences", func() {
		BeforeEach(func() {
			hvpa.Spec.Hpa.Template.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{}}
			matchErr = HaveOccurred()
		})

		It("should fail", func() {})
	})

	Describe("with default HPA template", func() {
		BeforeEach(func() {
			expectedHpa = &autoscaling.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: hvpa.Name + "-",
					Namespace:    hvpa.Namespace,
				},
				Spec: autoscaling.HorizontalPodAutoscalerSpec{
					MaxReplicas: hvpa.Spec.Hpa.Template.Spec.MaxReplicas,
					MinReplicas: hvpa.Spec.Hpa.Template.Spec.MinReplicas,
					ScaleTargetRef: autoscaling.CrossVersionObjectReference{
						APIVersion: hvpav1alpha1.SchemeGroupVersionHvpa.String(),
						Kind:       "Hvpa",
						Name:       hvpa.Name,
					},
					Metrics: append([]autoscaling.MetricSpec{}, hvpa.Spec.Hpa.Template.Spec.Metrics...),
				},
			}
		})

		It("should succeed", func() {})

		Describe("with labels", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.ObjectMeta.Labels = map[string]string{
					"key": "value",
				}

				expectedHpa.ObjectMeta.Labels = hvpa.Spec.Hpa.Template.ObjectMeta.DeepCopy().Labels
			})

			It("should use the specified labels", func() {})
		})

		Describe("with name", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.ObjectMeta.Name = "hpa"
			})

			It("should ignore the specified name", func() {})
		})

		Describe("with generateName", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.ObjectMeta.GenerateName = "hpa"
			})

			It("should ignore the specified generateName", func() {})
		})

		Describe("with maxReplicas", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.Spec.MaxReplicas = int32(4)
				expectedHpa.Spec.MaxReplicas = int32(4)
			})

			It("should use the specified maxReplicas", func() {})
		})

		Describe("with minReplicas", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.Spec.MinReplicas = pointer.Int32Ptr(4)
				expectedHpa.Spec.MinReplicas = pointer.Int32Ptr(4)
			})

			It("should use the specified minReplicas", func() {})
		})

		Describe("with metrics spec", func() {
			BeforeEach(func() {
				hvpa.Spec.Hpa.Template.Spec.Metrics = []autoscaling.MetricSpec{
					{
						Type: autoscaling.ResourceMetricSourceType,
						Resource: &autoscaling.ResourceMetricSource{
							Name:                     corev1.ResourceCPU,
							TargetAverageUtilization: pointer.Int32Ptr(80),
						},
					},
				}

				expectedHpa.Spec.Metrics = append([]autoscaling.MetricSpec{}, hvpa.Spec.Hpa.Template.Spec.Metrics...)
			})

			It("should use the specified metrics", func() {})
		})

		Describe("with targetRef", func() {
			BeforeEach(func() {
				hvpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ReplicationController",
					Name:       "target",
				}
			})

			It("should use the HVPA as the target in the HPA", func() {})
		})
	})
})
