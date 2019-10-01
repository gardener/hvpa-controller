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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

var _ = Describe("#Adopt VPA", func() {

	DescribeTable("##AdoptVPA",
		func(instance *autoscalingv1alpha1.Hvpa) {

			replica := int32(1)
			deploytest := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deploy-test-3",
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

			vpaList := &vpa_api.VerticalPodAutoscalerList{}
			Eventually(func() error {
				num := 0
				c.List(context.TODO(), vpaList)
				for _, obj := range vpaList.Items {
					if obj.GenerateName == "hvpa-3-" {
						num = num + 1
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Number of VPAs expected: 1; found %v", num)
			}, timeout).Should(Succeed())

			// Create new VPA for same HVPA
			newVpa, err := getVpaFromHvpa(instance)
			Expect(err).NotTo(HaveOccurred())
			err = c.Create(context.TODO(), newVpa)
			Expect(err).NotTo(HaveOccurred())

			// Eventually one of the VPAs should be garbage collected
			Eventually(func() error {
				num := 0
				c.List(context.TODO(), vpaList)
				for _, obj := range vpaList.Items {
					if obj.GenerateName == "hvpa-3-" {
						num = num + 1
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Number of VPAs expected: 1; found %v", num)
			}, timeout).Should(Succeed())
		},
		Entry("hvpa", newHvpa("hvpa-3", "deploy-test-3", "label-3")),
	)
})
