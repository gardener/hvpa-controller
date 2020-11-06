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

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/onsi/gomega"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// These tests are written in BDD-style using Ginkgo framework. Refer to
// http://onsi.github.io/ginkgo to learn more.

var _ = Describe("Hvpa", func() {
	var (
		key              types.NamespacedName
		created, fetched *Hvpa
	)

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	// Add Tests for OpenAPI validation (or additional CRD features) specified in
	// your API definition.
	// Avoid adding tests for vanilla CRUD operations because they would
	// test Kubernetes API server, which isn't the goal here.
	Context("Create API", func() {
		It("should create an object successfully", func() {
			var (
				minReplicas int32 = 1
				maxReplicas int32 = 2
			)

			key = types.NamespacedName{
				Name:      "foo",
				Namespace: "default",
			}
			created = &Hvpa{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: HvpaSpec{
					TargetRef: &autoscaling.CrossVersionObjectReference{},
					Hpa: HpaSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test-label": "test-label",
							},
						},
						Template: HpaTemplate{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"test-label": "test-label",
								},
							},
							Spec: HpaTemplateSpec{
								MinReplicas: &minReplicas,
								MaxReplicas: maxReplicas,
							},
						},
					},
					Vpa: VpaSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test-label": "test-label",
							},
						},
						Template: VpaTemplate{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"test-label": "test-label",
								},
							},
						},
					},
				},
			}
			By("creating an API obj")

			// Test Create
			fetched = &Hvpa{}
			Expect(k8sClient.Create(context.TODO(), created)).NotTo(HaveOccurred())

			Expect(k8sClient.Get(context.TODO(), key, fetched)).NotTo(HaveOccurred())
			Expect(fetched).To(gomega.Equal(created))
			// Test Updating the Labels
			updated := fetched.DeepCopy()
			updated.Labels = map[string]string{"hello": "world"}
			By("updating the labels")
			Expect(k8sClient.Update(context.TODO(), updated)).NotTo(HaveOccurred())

			Expect(k8sClient.Get(context.TODO(), key, fetched)).NotTo(HaveOccurred())
			Expect(fetched).To(Equal(updated))

			// Test Delete
			By("Deleting the fetched object")
			Expect(k8sClient.Delete(context.TODO(), fetched)).NotTo(HaveOccurred())
			Expect(k8sClient.Get(context.TODO(), key, fetched)).To(HaveOccurred())
		})
	})
})
