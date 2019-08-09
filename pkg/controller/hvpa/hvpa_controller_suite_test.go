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

package hvpa

import (
	stdlog "log"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gardener/hvpa-controller/pkg/apis"
	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/pkg/apis/autoscaling/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var cfg *rest.Config

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	e := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crds")},
	}
	apis.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = e.Start(); err != nil {
		stdlog.Fatal(err)
	}

	RunSpecs(t, "HVPA Controller Manager Suite")
	e.Stop()
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(req)
		requests <- req
		return result, err
	})
	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, g *GomegaWithT) (chan struct{}, *sync.WaitGroup) {
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		Expect(mgr.Start(stop)).NotTo(HaveOccurred())
	}()
	return stop, wg
}

func newHvpa() *autoscalingv1alpha1.Hvpa {
	replica := int32(1)
	util := int32(70)
	updateMode := autoscalingv1alpha1.UpdateModeOn

	instance := &autoscalingv1alpha1.Hvpa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: autoscalingv1alpha1.HvpaSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "deploy-test",
			},
			HpaTemplate: autoscalingv1alpha1.HpaTemplateSpec{
				MinReplicas: &replica,
				MaxReplicas: 2,
				UpdatePolicy: &autoscalingv1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
				Metrics: []autoscaling.MetricSpec{
					autoscaling.MetricSpec{
						Type: autoscaling.ResourceMetricSourceType,
						Resource: &autoscaling.ResourceMetricSource{
							Name: v1.ResourceCPU,
							Target: autoscaling.MetricTarget{
								Type:               autoscaling.UtilizationMetricType,
								AverageUtilization: &util,
							},
						},
					},
				},
			},
			VpaTemplate: autoscalingv1alpha1.VpaTemplateSpec{
				UpdatePolicy: &autoscalingv1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
			},
		},
	}

	return instance
}
