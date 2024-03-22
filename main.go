// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0


package main

import (
	"flag"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	klogv2 "k8s.io/klog/v2"
	k8sautoscalingv2 "k8s.io/kubernetes/pkg/apis/autoscaling/v2"
	k8sautoscalingv2beta1 "k8s.io/kubernetes/pkg/apis/autoscaling/v2beta1"
	ctrl "sigs.k8s.io/controller-runtime"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/gardener/hvpa-controller/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
	utilruntime.Must(k8sautoscalingv2beta1.RegisterConversions(scheme))
	utilruntime.Must(k8sautoscalingv2.RegisterConversions(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	klogv2.InitFlags(nil)

	var (
		metricsAddr           string
		enableLeaderElection  bool
		enableDetailedMetrics bool
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9569", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableDetailedMetrics, "enable-detailed-metrics", false,
		"Enable detailed per HVPA resource metrics. This could significantly increase the cardinality of the metrics.")
	flag.Parse()

	ctrl.SetLogger(klogv2.NewKlogr())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "hvpa-controller",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	foundAutoscalingV2 := false

	dc, _ := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	groups, _ := dc.ServerGroups()
	apiVersions := metav1.ExtractGroupVersions(groups)

	for _, apiVersion := range apiVersions {
		if apiVersion == "autoscaling/v2" {
			foundAutoscalingV2 = true
			break
		}
	}

	if err = (&controllers.HvpaReconciler{
		Client:                 mgr.GetClient(),
		Scheme:                 mgr.GetScheme(),
		EnableDetailedMetrics:  enableDetailedMetrics,
		IsAutoscalingV2Enabled: foundAutoscalingV2,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Hvpa")
		os.Exit(1)
	}

	// TODO: Enable webhooks
	/*
		if err = (&autoscalingv1alpha1.Hvpa{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Hvpa")
			os.Exit(1)
		}
	*/

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
