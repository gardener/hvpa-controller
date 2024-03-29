// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0


package controllers

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	k8sautoscalingv2 "k8s.io/kubernetes/pkg/apis/autoscaling/v2"
	k8sautoscalingv2beta1 "k8s.io/kubernetes/pkg/apis/autoscaling/v2beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg               *rest.Config
	k8sClient         client.Client
	testEnv           *envtest.Environment
	mgr               manager.Manager
	managerCancelFunc context.CancelFunc
	reconciler        HvpaReconciler
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
	done := make(chan interface{})
	go func() {
		ctx := context.Background()
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "config", "crd", "integration_test"),
				filepath.Join("..", "config", "crd", "output"),
			},
		}

		var err error
		cfg, err = testEnv.Start()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).ToNot(BeNil())

		s := scheme.Scheme

		err = autoscalingv1alpha1.AddToScheme(s)
		err = k8sautoscalingv2beta1.RegisterConversions(s)
		err = k8sautoscalingv2.RegisterConversions(s)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		k8sClient, err = client.New(cfg, client.Options{Scheme: s})
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sClient).ToNot(BeNil())

		// Setup the Manager.
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())

		dc, _ := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
		groups, _ := dc.ServerGroups()
		apiVersions := metav1.ExtractGroupVersions(groups)

		var foundAutoscalingV2 bool
		for _, apiVersion := range apiVersions {
			if apiVersion == "autoscaling/v2" {
				foundAutoscalingV2 = true
				break
			}
		}

		reconciler = HvpaReconciler{
			Client:                 mgr.GetClient(),
			Scheme:                 mgr.GetScheme(),
			IsAutoscalingV2Enabled: foundAutoscalingV2,
		}

		err = reconciler.SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())

		managerCancelFunc = StartTestManager(ctx, mgr)

		close(done)
	}()
	Eventually(done, "60s").Should(BeClosed())
})

var _ = AfterSuite(func() {
	managerCancelFunc()

	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		requests <- req
		return result, err
	})
	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(ctx context.Context, mgr manager.Manager) context.CancelFunc {
	c, cancelFunc := context.WithCancel(ctx)
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(c)).To(Succeed())
	}()
	return cancelFunc
}

func newHvpa(name, target, labelVal string, minChange autoscalingv1alpha1.ScaleParams) *autoscalingv1alpha1.Hvpa {
	replica := int32(1)
	util := int32(70)

	stabilizationDur := "3m"

	updateMode := autoscalingv1alpha1.UpdateModeAuto

	instance := &autoscalingv1alpha1.Hvpa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"hpa-controller": "hvpa",
			},
		},
		Spec: autoscalingv1alpha1.HvpaSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       target,
				APIVersion: "apps/v1",
			},
			Hpa: autoscalingv1alpha1.HpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"hpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: autoscalingv1alpha1.ScaleType{
					UpdatePolicy: autoscalingv1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},
				ScaleDown: autoscalingv1alpha1.ScaleType{
					UpdatePolicy: autoscalingv1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
				},

				Template: autoscalingv1alpha1.HpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"hpaKey": labelVal,
						},
					},
					Spec: autoscalingv1alpha1.HpaTemplateSpec{
						MinReplicas: &replica,
						MaxReplicas: 3,
						Metrics: []autoscaling.MetricSpec{
							{
								Type: autoscaling.ResourceMetricSourceType,
								Resource: &autoscaling.ResourceMetricSource{
									Name:                     v1.ResourceCPU,
									TargetAverageUtilization: &util,
								},
							},
						},
					},
				},
			},
			Vpa: autoscalingv1alpha1.VpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vpaKey": labelVal,
					},
				},
				Deploy: true,
				ScaleUp: autoscalingv1alpha1.ScaleType{
					UpdatePolicy: autoscalingv1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange:             minChange,
				},
				ScaleDown: autoscalingv1alpha1.ScaleType{
					UpdatePolicy: autoscalingv1alpha1.UpdatePolicy{
						UpdateMode: &updateMode,
					},
					StabilizationDuration: &stabilizationDur,
					MinChange:             minChange,
				},
				Template: autoscalingv1alpha1.VpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"vpaKey": labelVal,
						},
					},
				},
			},
			WeightBasedScalingIntervals: []autoscalingv1alpha1.WeightBasedScalingInterval{
				{
					StartReplicaCount: 1,
					LastReplicaCount:  2,
					VpaWeight:         30,
				},
				{
					StartReplicaCount: 2,
					LastReplicaCount:  3,
					VpaWeight:         80,
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
						v1.Container{
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
