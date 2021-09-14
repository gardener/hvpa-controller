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
	hvpav1alpha2 "github.com/gardener/hvpa-controller/api/v1alpha2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

var _ = Describe("getVpaFromHvpa", func() {
	var (
		hvpa        *hvpav1alpha2.Hvpa
		expectedVpa *vpa_api.VerticalPodAutoscaler
		matchErr    gomegatypes.GomegaMatcher
	)

	BeforeEach(func() {
		hvpa = &hvpav1alpha2.Hvpa{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "instance",
				Namespace: "default",
			},
			Spec: hvpav1alpha2.HvpaSpec{
				TargetRef: &autoscalingv2beta1.CrossVersionObjectReference{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ReplicationController",
					Name:       "target",
				},
			},
		}

		expectedVpa = nil
		matchErr = Succeed()
	})

	JustBeforeEach(func() {
		var vpa, err = getVpaFromHvpa(hvpa)

		Expect(err).To(matchErr)
		if expectedVpa == nil {
			Expect(vpa).To(BeNil())
		} else {
			Expect(vpa).To(Equal(expectedVpa))
		}
	})

	Describe("when VPA template has ownerReferences", func() {
		BeforeEach(func() {
			hvpa.Spec.Vpa.Template.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{}}
			matchErr = HaveOccurred()
		})

		It("should fail", func() {})
	})

	Describe("with default VPA template", func() {
		BeforeEach(func() {
			expectedVpa = &vpa_api.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: hvpa.Name + "-",
					Namespace:    hvpa.Namespace,
				},
				Spec: vpa_api.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						APIVersion: hvpa.Spec.TargetRef.APIVersion,
						Kind:       hvpa.Spec.TargetRef.Kind,
						Name:       hvpa.Spec.TargetRef.Name,
					},
					UpdatePolicy: &vpa_api.PodUpdatePolicy{
						UpdateMode: func() *vpa_api.UpdateMode {
							var um = vpa_api.UpdateModeOff
							return &um
						}(),
					},
				},
			}
		})

		It("should succeed", func() {})

		Describe("with labels", func() {
			BeforeEach(func() {
				hvpa.Spec.Vpa.Template.ObjectMeta.Labels = map[string]string{
					"key": "value",
				}

				expectedVpa.ObjectMeta.Labels = hvpa.Spec.Vpa.Template.ObjectMeta.DeepCopy().Labels
			})

			It("should use the specified labels", func() {})
		})

		Describe("with name", func() {
			BeforeEach(func() {
				hvpa.Spec.Vpa.Template.ObjectMeta.Name = "vpa"
			})

			It("should ignore the specified name", func() {})
		})

		Describe("with generateName", func() {
			BeforeEach(func() {
				hvpa.Spec.Vpa.Template.ObjectMeta.GenerateName = "vpa"
			})

			It("should ignore the specified generateName", func() {})
		})

		Describe("with resourcePolicy", func() {
			BeforeEach(func() {
				hvpa.Spec.Vpa.Template.Spec.ResourcePolicy = &vpa_api.PodResourcePolicy{
					ContainerPolicies: []vpa_api.ContainerResourcePolicy{
						{
							ContainerName: "*",
							Mode: func() *vpa_api.ContainerScalingMode {
								var csm = vpa_api.ContainerScalingModeOff
								return &csm
							}(),
							MinAllowed: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("300m"),
							},
							MaxAllowed: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1G"),
							},
						},
					},
				}

				expectedVpa.Spec.ResourcePolicy = hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.DeepCopy()
			})

			It("should use the specified resourcePolicy", func() {})
		})
	})
})
