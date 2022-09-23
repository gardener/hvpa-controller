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

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("#Adopt VPA", func() {

	DescribeTable("##AdoptVPA",
		func(instance *autoscalingv1alpha1.Hvpa) {
			deploytest := target.DeepCopy()
			// Overwrite name
			deploytest.Name = "deploy-test-3"

			c := mgr.GetClient()
			// Create the test deployment
			Expect(c.Create(context.TODO(), deploytest)).To(Succeed())

			// Create the Hvpa object and expect the Reconcile and VPA to be created
			Expect(c.Create(context.TODO(), instance)).To(Succeed())
			defer c.Delete(context.TODO(), instance)

			hasSingleChildFn := func() error {
				num := 0
				objList := &vpa_api.VerticalPodAutoscalerList{}
				if err := c.List(context.TODO(), objList); err != nil {
					return err
				}
				for _, obj := range objList.Items {
					for _, owner := range obj.GetOwnerReferences() {
						if owner.UID == instance.GetUID() {
							num = num + 1
						}
					}
				}
				if num == 1 {
					return nil
				}
				return fmt.Errorf("Error: Number of VPAs expected: 1; found %v", num)
			}

			Eventually(hasSingleChildFn, timeout).Should(Succeed())

			// Create new VPA for same HVPA
			newVpa, err := getVpaFromHvpa(instance)
			Expect(err).NotTo(HaveOccurred())
			err = c.Create(context.TODO(), newVpa)
			Expect(err).NotTo(HaveOccurred())

			// Eventually one of the VPAs should be garbage collected
			Eventually(hasSingleChildFn, timeout).Should(Succeed())

			// Create new VPA for same HVPA
			newVpa, err = getVpaFromHvpa(instance)
			Expect(err).NotTo(HaveOccurred())

			Expect(controllerutil.SetControllerReference(instance, newVpa, mgr.GetScheme())).To(Succeed())

			// Replace the labels. The HVPA controller should remove the owner reference
			label := make(map[string]string)
			label["orphanKeyVpa"] = "orphanValueVpa"
			newVpa.SetLabels(label)

			Expect(c.Create(context.TODO(), newVpa)).To(Succeed())

			// Eventually the owner ref from VPA should be removed by the HVPA controller
			Eventually(func() error {
				vpaList := &vpa_api.VerticalPodAutoscalerList{}
				c.List(context.TODO(), vpaList, client.MatchingLabels(label))
				for _, obj := range vpaList.Items {
					for _, ref := range obj.GetOwnerReferences() {
						if ref.UID == instance.GetUID() {
							return fmt.Errorf("Error: VPA with label %v not released by HVPA %v", label, instance.Name)
						}
					}
				}
				return nil
			}, timeout).Should(Succeed())
		},
		Entry("vpa", newHvpa("hvpa-3", "deploy-test-3", "label-3", minChange)),
	)
})
