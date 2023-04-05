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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
)

var _ = Describe("#Adopt HPA", func() {

	DescribeTable("##AdoptHPA",
		func(instance *autoscalingv1alpha1.Hvpa) {
			deploytest := target.DeepCopy()
			// Overwrite name
			deploytest.Name = "deploy-test-2"

			c := mgr.GetClient()
			// Create the test deployment
			Expect(c.Create(context.TODO(), deploytest)).To(Succeed())

			// Create the Hvpa object and expect the Reconcile and HPA to be created
			Expect(c.Create(context.TODO(), instance)).To(Succeed())
			defer c.Delete(context.TODO(), instance)

			hasSingleChildFn := func() error {
				num := 0
				objList := &autoscaling.HorizontalPodAutoscalerList{}
				if !reconciler.IsAutoscalingV2Enabled {
					if err := c.List(context.TODO(), objList); err != nil {
						return err
					}
				} else {
					v2HpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
					if err := c.List(context.TODO(), v2HpaList); err != nil {
						return err
					}
					objList, _ = reconciler.Convert_v2_HorizontalPodAutoscalerList_To_v2beta1(v2HpaList)
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

			// Create new HPA for same HVPA
			newHpa, err := getHpaFromHvpa(instance)
			Expect(err).NotTo(HaveOccurred())
			if !reconciler.IsAutoscalingV2Enabled {
				err = c.Create(context.TODO(), newHpa)
				Expect(err).NotTo(HaveOccurred())
			} else {
				v2NewHpa, err := reconciler.Convert_v2beta1_HPA_to_v2(newHpa)
				err = c.Create(context.TODO(), v2NewHpa)
				Expect(err).NotTo(HaveOccurred())
			}

			// Eventually one of the HPAs should be garbage collected
			Eventually(hasSingleChildFn, timeout).Should(Succeed())

			// Create new HPA for same HVPA
			newHpa, err = getHpaFromHvpa(instance)
			Expect(err).NotTo(HaveOccurred())

			Expect(controllerutil.SetControllerReference(instance, newHpa, mgr.GetScheme())).To(Succeed())

			// Replace the labels. The HVPA controller should remove the owner reference
			label := make(map[string]string)
			label["orphanKeyHpa"] = "orphanValueHpa"
			newHpa.SetLabels(label)

			if !reconciler.IsAutoscalingV2Enabled {
				err = c.Create(context.TODO(), newHpa)
				Expect(err).NotTo(HaveOccurred())
			} else {
				v2NewHpa, err := reconciler.Convert_v2beta1_HPA_to_v2(newHpa)
				err = c.Create(context.TODO(), v2NewHpa)
				Expect(err).NotTo(HaveOccurred())
			}

			// Eventually the owner ref from HPA should be removed by the HVPA controller
			Eventually(func() error {
				hpaList := &autoscaling.HorizontalPodAutoscalerList{}
				if !reconciler.IsAutoscalingV2Enabled {
					c.List(context.TODO(), hpaList, client.MatchingLabels(label))
				} else {
					v2HpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
					c.List(context.TODO(), v2HpaList, client.MatchingLabels(label))
					hpaList, _ = reconciler.Convert_v2_HorizontalPodAutoscalerList_To_v2beta1(v2HpaList)
				}
				for _, obj := range hpaList.Items {
					for _, ref := range obj.GetOwnerReferences() {
						if ref.UID == instance.GetUID() {
							return fmt.Errorf("Error: HPA with label %v not released by HVPA %v", label, instance.Name)
						}
					}
				}
				return nil
			}, timeout).Should(Succeed())
		},
		Entry("hpa", newHvpa("hvpa-2", "deploy-test-2", "label-2", minChange)),
	)
})
