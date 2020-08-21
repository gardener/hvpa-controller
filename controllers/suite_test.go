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
	"path/filepath"
	"sync"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var mgr manager.Manager
var stopMgr chan struct{}
var mgrStopped *sync.WaitGroup

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{envtest.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = hvpav1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	// Setup the Manager.
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{})
	Expect(err).NotTo(HaveOccurred())

	reconciler := HvpaReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		EnableDetailedMetrics: true,
	}

	err = reconciler.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	stopMgr, mgrStopped = StartTestManager(mgr, &GomegaWithT{})

	close(done)
}, 60)

var _ = AfterSuite(func() {
	close(stopMgr)
	mgrStopped.Wait()

	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

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

/* This hvpa results in following effective buckets after taking ScalingIntervalsOverlap into account:
 * {map[cpu:{10 1000} memory:{50M 2G} replicas:{1}]}
 * {map[cpu:{400 5300} memory:{990M 8.2425G} replicas:{2}]}
 * {map[cpu:{2827 10200} memory:{5.485G 15.495G} replicas:{3}]}
 * {map[cpu:{6120 15100} memory:{11.61125G 22.7475G} replicas:{4}]}
 * {map[cpu:{9664 20000} memory:{18.188G 30G} replicas:{5}]}
 * {map[cpu:{13333 30000} memory:{24.99 40G} replicas:{6}]}
 */
func newHvpa(name, target, labelVal string, minChange hvpav1alpha1.ScaleParams) *hvpav1alpha1.Hvpa {
	updateMode := hvpav1alpha1.UpdateModeAuto

	instance := &hvpav1alpha1.Hvpa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"hpa-controller": "hvpa",
			},
		},
		Spec: hvpav1alpha1.HvpaSpec{
			Replicas: int32Ptr(1),
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       target,
				APIVersion: "apps/v1",
			},
			Hpa: hvpav1alpha1.HpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"hpaKey": labelVal,
					},
				},
				Deploy: true,
				Template: hvpav1alpha1.HpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"hpaKey": labelVal,
						},
					},
					Spec: hvpav1alpha1.HpaTemplateSpec{
						MinReplicas: int32Ptr(1),
						MaxReplicas: 3,
						Metrics: []autoscaling.MetricSpec{
							{
								Type: autoscaling.ResourceMetricSourceType,
								Resource: &autoscaling.ResourceMetricSource{
									Name:                     v1.ResourceCPU,
									TargetAverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			Vpa: hvpav1alpha1.VpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vpaKey": labelVal,
					},
				},
				Deploy: true,
				Template: hvpav1alpha1.VpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"vpaKey": labelVal,
						},
					},
					Spec: hvpav1alpha1.VpaTemplateSpec{
						ResourcePolicy: &vpa_api.PodResourcePolicy{
							ContainerPolicies: []vpa_api.ContainerResourcePolicy{
								{
									ContainerName: target,
									MinAllowed: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("10m"),
										v1.ResourceMemory: resource.MustParse("50M"),
									},
									MaxAllowed: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("2"),
										v1.ResourceMemory: resource.MustParse("5G"),
									},
								},
							},
						},
					},
				},
			},
			ScaleUp: hvpav1alpha1.ScaleType{
				UpdatePolicy: hvpav1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
				StabilizationDuration: stringPtr("3m"),
				MinChange:             minChange,
			},
			ScaleDown: hvpav1alpha1.ScaleType{
				UpdatePolicy: hvpav1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
				StabilizationDuration: stringPtr("3m"),
				MinChange:             minChange,
			},
			ScaleIntervals: []hvpav1alpha1.ScaleIntervals{
				{
					MaxCPU:      resourcePtr("1"),
					MaxMemory:   resourcePtr("2G"),
					MaxReplicas: 1,
				},
				{
					MaxCPU:      resourcePtr("20"),
					MaxMemory:   resourcePtr("30G"),
					MaxReplicas: 5,
				},
				{
					MaxCPU:      resourcePtr("30"),
					MaxMemory:   resourcePtr("40G"),
					MaxReplicas: 6,
				},
			},
			ScalingIntervalsOverlap: hvpav1alpha1.ResourceChangeParams{
				v1.ResourceCPU: hvpav1alpha1.ChangeParams{
					Percentage: int32Ptr(20),
				},
				v1.ResourceMemory: hvpav1alpha1.ChangeParams{
					Value: stringPtr("10M"),
				},
			},
		},
	}

	return instance
}

func newHpaStatus(currentReplicas, desiredReplicas int32, conditions []autoscaling.HorizontalPodAutoscalerCondition) *autoscaling.HorizontalPodAutoscalerStatus {
	return &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
		Conditions:      conditions,
	}
}

func newVpaStatus(containerName, mem, cpu string) *vpa_api.VerticalPodAutoscalerStatus {

	return &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: []vpa_api.RecommendedContainerResources{
				{
					ContainerName: containerName,
					Target: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse(cpu),
						v1.ResourceMemory: resource.MustParse(mem),
					},
				},
			},
		},
		Conditions: []vpa_api.VerticalPodAutoscalerCondition{
			{
				Status: "True",
				Type:   vpa_api.RecommendationProvided,
			},
			{
				Status: "False",
				Type:   vpa_api.ConfigUnsupported,
			},
		},
	}
}

func newTarget(name string, resources v1.ResourceRequirements, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": name,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      name,
							Image:     "k8s.gcr.io/pause-amd64:3.0",
							Resources: resources,
						},
					},
				},
			},
		},
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}

func int32Ptr(i int32) *int32 {
	return &i
}

func stringPtr(i string) *string {
	return &i
}

func resourcePtr(i string) *resource.Quantity {
	q := resource.MustParse(i)
	return &q
}
