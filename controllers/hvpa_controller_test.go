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
	"errors"
	"fmt"
	"time"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo", Namespace: "default"}}
var hpaKey = types.NamespacedName{Name: "foo-hpa", Namespace: "default"}
var vpaKey = types.NamespacedName{Name: "foo-vpa", Namespace: "default"}

const timeout = time.Second * 5

var _ = Describe("#TestReconcile", func() {

	DescribeTable("##ReconcileHPAandVPA",
		func(instance *autoscalingv1alpha1.Hvpa) {

			replica := int32(1)
			deploytest := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deploy-test",
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
				Status: appsv1.DeploymentStatus{
					Replicas: replica,
				},
			}
			// Setup the Manager.
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{})
			Expect(err).NotTo(HaveOccurred())

			reconciler := HvpaReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}

			err = reconciler.SetupWithManager(mgr)
			Expect(err).NotTo(HaveOccurred())

			c = mgr.GetClient()
			stopMgr, mgrStopped := StartTestManager(mgr, &GomegaWithT{})

			defer func() {
				close(stopMgr)
				mgrStopped.Wait()
			}()

			// Create the test deployment
			Expect(c.Create(context.TODO(), deploytest)).To(Succeed())
			Expect(c.Status().Update(context.TODO(), deploytest)).To(Succeed()) //Update deployment status

			// Create the Hvpa object and expect the Reconcile and HPA to be created
			Expect(c.Create(context.TODO(), instance)).To(Succeed())
			defer c.Delete(context.TODO(), instance)

			Eventually(func() error {
				hvpa := &autoscalingv1alpha1.Hvpa{}
				if err := c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
					return err
				}

				if hvpa.Status.TargetSelector == nil {
					return errors.New(".status.TargetSelector is nil")
				}
				if selector, err := metav1.LabelSelectorAsSelector(deploytest.Spec.Selector); err != nil {
					return err
				} else if selector == nil {
					return errors.New("selector is nil")
				} else if *hvpa.Status.TargetSelector != selector.String() {
					return fmt.Errorf("Expected .status.TargetSelector to be %s but was %s", selector.String(), *hvpa.Status.TargetSelector)
				}

				if hvpa.Status.Replicas == nil {
					return errors.New(".status.Replicas is nil")
				} else if *hvpa.Status.Replicas != replica {
					return fmt.Errorf("Expected .status.Replicas to be %d but was %d", replica, *hvpa.Status.Replicas)
				}

				return nil

			}, "2s").Should(Succeed())

			hpa := &autoscaling.HorizontalPodAutoscaler{}
			Eventually(func() error { return c.Get(context.TODO(), hpaKey, hpa) }, timeout).
				Should(Succeed())

			vpa := &vpa_api.VerticalPodAutoscaler{}
			Eventually(func() error { return c.Get(context.TODO(), vpaKey, vpa) }, timeout).
				Should(Succeed())

			// Delete the HPA and expect Reconcile to be called for HPA deletion
			Expect(c.Delete(context.TODO(), hpa)).NotTo(HaveOccurred())
			Eventually(func() error { return c.Get(context.TODO(), hpaKey, hpa) }, timeout).
				Should(Succeed())

			// Manually delete HPA & VPA since GC isn't enabled in the test control plane
			Eventually(func() error { return c.Delete(context.TODO(), hpa) }, timeout).
				Should(MatchError("horizontalpodautoscalers.autoscaling \"foo-hpa\" not found"))
			Eventually(func() error { return c.Delete(context.TODO(), vpa) }, timeout).
				Should(MatchError("verticalpodautoscalers.autoscaling.k8s.io \"foo-vpa\" not found"))

			// Delete the test deployment
			Expect(c.Delete(context.TODO(), deploytest)).NotTo(HaveOccurred())
		},
		Entry("hvpa", newHvpa()),
	)
})
