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
	"fmt"
	"math"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace              = "hvpa"
	metricsSubsystemAggregate     = "aggregate"
	metricsSubsystemSpec          = "spec"
	metricsSubsystemStatus        = "status"
	labelNamespace                = "namespace"
	labelName                     = "name"
	labelReason                   = "reason"
	labelHpaScaleUpUpdatePolicy   = "hpaScaleUpUpdatePolicy"
	labelHpaScaleDownUpdatePolicy = "hpaScaleDownUpdatePolicy"
	labelVpaScaleUpUpdatePolicy   = "vpaScaleUpUpdatePolicy"
	labelVpaScaleDownUpdatePolicy = "vpaScaleDownUpdatePolicy"
	labelTargetRefName            = "targetName"
	labelTargetRefKind            = "targetKind"
	labelContainer                = "container"
	labelResource                 = "resource"
	labelRecommendation           = "recommendation"
	recoTarget                    = "target"
	recoLowerBound                = "lowerBound"
	recoUpperBound                = "upperBound"
	recoUncappedTarget            = "uncappedTarget"
)

type hvpaMetrics struct {
	aggrAppliedScalingsTotal        *prometheus.GaugeVec
	aggrBlockedScalingsTotal        *prometheus.GaugeVec
	specReplicas                    *prometheus.GaugeVec
	statusReplicas                  *prometheus.GaugeVec
	statusAppliedHPACurrentReplicas *prometheus.GaugeVec
	statusAppliedHPADesiredReplicas *prometheus.GaugeVec
	statusAppliedVPARecommendation  *prometheus.GaugeVec
	statusBlockedHPACurrentReplicas *prometheus.GaugeVec
	statusBlockedHPADesiredReplicas *prometheus.GaugeVec
	statusBlockedVPARecommendation  *prometheus.GaugeVec
}

// AddMetrics initializes and registers the custom metrics for HVPA controller.
func (r *HvpaReconciler) AddMetrics() error {
	var (
		m             = &hvpaMetrics{}
		allCollectors []prometheus.Collector
	)
	m.aggrAppliedScalingsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystemAggregate,
			Name:      "applied_scaling_total",
			Help:      "The number of scalings applied by the HVPA controller.",
		},
		nil,
	)
	m.aggrAppliedScalingsTotal.With(nil).Set(0)
	allCollectors = append(allCollectors, m.aggrAppliedScalingsTotal)

	m.aggrBlockedScalingsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystemAggregate,
			Name:      "blocked_scalings_total",
			Help:      "The number of scalings blocked by the HVPA controller.",
		},
		[]string{labelReason},
	)
	for _, reason := range []hvpav1alpha1.BlockingReason{hvpav1alpha1.BlockingReasonMaintenanceWindow, hvpav1alpha1.BlockingReasonMinChange, hvpav1alpha1.BlockingReasonStabilizationWindow, hvpav1alpha1.BlockingReasonUpdatePolicy, hvpav1alpha1.BlockingReasonWeight} {
		m.aggrBlockedScalingsTotal.WithLabelValues(string(reason)).Set(0)
	}
	allCollectors = append(allCollectors, m.aggrBlockedScalingsTotal)

	if r.EnableDetailedMetrics {
		m.specReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemSpec,
				Name:      "replicas",
				Help:      "The number of replicas in the HVPA spec (part of the Scale sub-resource).",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.specReplicas)

		m.statusReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The number of replicas in the HVPA status (part of the Scale sub-resource).",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusReplicas)

		m.statusAppliedHPACurrentReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "applied_hpa_current_replicas",
				Help:      "The applied current replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusAppliedHPACurrentReplicas)

		m.statusAppliedHPADesiredReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "applied_hpa_desired_replicas",
				Help:      "The applied desired replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusAppliedHPADesiredReplicas)

		m.statusAppliedVPARecommendation = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "applied_vpa_recommendation",
				Help:      "The applied recommendation from VPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy, labelContainer, labelRecommendation, labelResource},
		)
		allCollectors = append(allCollectors, m.statusAppliedVPARecommendation)

		m.statusBlockedHPACurrentReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "blocked_hpa_current_replicas",
				Help:      "The blocked current replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy, labelReason},
		)
		allCollectors = append(allCollectors, m.statusBlockedHPACurrentReplicas)

		m.statusBlockedHPADesiredReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "blocked_hpa_desired_replicas",
				Help:      "The blocked desired replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy, labelReason},
		)
		allCollectors = append(allCollectors, m.statusBlockedHPADesiredReplicas)

		m.statusBlockedVPARecommendation = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "blocked_vpa_recommendation",
				Help:      "The blocked recommendation from VPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaScaleUpUpdatePolicy, labelHpaScaleDownUpdatePolicy, labelVpaScaleUpUpdatePolicy, labelVpaScaleDownUpdatePolicy, labelReason, labelContainer, labelRecommendation, labelResource},
		)
		allCollectors = append(allCollectors, m.statusBlockedVPARecommendation)
	}

	r.metrics = m

	for _, c := range allCollectors {
		if err := metrics.Registry.Register(c); err != nil {
			return err
		}
	}

	return nil
}

func copyLabels(s prometheus.Labels) prometheus.Labels {
	if s == nil {
		return nil
	}

	t := prometheus.Labels{}
	for k := range s {
		t[k] = s[k]
	}

	return t
}

func basicLabels(hvpa *hvpav1alpha1.Hvpa) prometheus.Labels {
	return prometheus.Labels{
		labelNamespace:     hvpa.Namespace,
		labelName:          hvpa.Name,
		labelTargetRefKind: hvpa.Spec.TargetRef.Kind,
		labelTargetRefName: hvpa.Spec.TargetRef.Name,
	}
}

func basicDetailedLabels(hvpa *hvpav1alpha1.Hvpa) prometheus.Labels {
	l := basicLabels(hvpa)

	// Initialise to default update mode
	l[labelHpaScaleUpUpdatePolicy] = hvpav1alpha1.UpdateModeDefault
	l[labelHpaScaleDownUpdatePolicy] = hvpav1alpha1.UpdateModeDefault
	l[labelVpaScaleUpUpdatePolicy] = hvpav1alpha1.UpdateModeDefault
	l[labelVpaScaleDownUpdatePolicy] = hvpav1alpha1.UpdateModeDefault

	if hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode != nil {
		l[labelHpaScaleUpUpdatePolicy] = *hvpa.Spec.Hpa.ScaleUp.UpdatePolicy.UpdateMode
	}
	if hvpa.Spec.Hpa.ScaleDown.UpdatePolicy.UpdateMode != nil {
		l[labelHpaScaleDownUpdatePolicy] = *hvpa.Spec.Hpa.ScaleDown.UpdatePolicy.UpdateMode
	}

	if hvpa.Spec.Vpa.ScaleUp.UpdatePolicy.UpdateMode != nil {
		l[labelVpaScaleUpUpdatePolicy] = *hvpa.Spec.Vpa.ScaleUp.UpdatePolicy.UpdateMode
	}
	if hvpa.Spec.Vpa.ScaleDown.UpdatePolicy.UpdateMode != nil {
		l[labelVpaScaleDownUpdatePolicy] = *hvpa.Spec.Vpa.ScaleDown.UpdatePolicy.UpdateMode
	}
	return l
}

func basicBlockedLabels(hvpa *hvpav1alpha1.Hvpa, reason hvpav1alpha1.BlockingReason) prometheus.Labels {
	l := basicDetailedLabels(hvpa)
	l[labelReason] = string(reason)
	return l
}

func updateVPARecommendations(gv *prometheus.GaugeVec, containers []string, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, baseLabelsFn func() prometheus.Labels) {
	if gv == nil || vpaStatus == nil || vpaStatus.Recommendation == nil {
		return
	}

	updateReco := func(container, recommendation string, resourceList v1.ResourceList) {
		for _, resource := range allContainerResourceNames {
			l := baseLabelsFn()
			l[labelContainer] = container
			l[labelRecommendation] = recommendation
			l[labelResource] = string(resource)

			if resourceList == nil {
				gv.With(l).Set(math.NaN())
			} else if q, ok := resourceList[resource]; !ok {
				gv.With(l).Set(math.NaN())
			} else {
				if resource == v1.ResourceCPU {
					gv.With(l).Set(float64(q.MilliValue()))
				} else {
					gv.With(l).Set(float64(q.Value()))
				}
			}
		}
	}

	recoMap := make(map[string]*vpa_api.RecommendedContainerResources)
	for i := range vpaStatus.Recommendation.ContainerRecommendations {
		cr := &vpaStatus.Recommendation.ContainerRecommendations[i]
		recoMap[cr.ContainerName] = cr
	}

	for _, c := range containers {
		if cr, ok := recoMap[c]; ok {
			updateReco(c, recoTarget, cr.Target)
		} else {
			updateReco(c, recoTarget, nil)
		}
		/* To be enabled later
		updateReco(cr.ContainerName, recoLowerBound, cr.LowerBound)
		updateReco(cr.ContainerName, recoUpperBound, cr.UpperBound)
		updateReco(cr.ContainerName, recoUncappedTarget, cr.UncappedTarget)
		*/
	}
}

var allContainerResourceNames = [...]v1.ResourceName{
	v1.ResourceCPU,
	v1.ResourceMemory,
}

func deleteVPARecommendations(gv *prometheus.GaugeVec, containers []string, baseLabelsFn func() prometheus.Labels) {
	if gv == nil || len(containers) <= 0 {
		return
	}

	deleteReco := func(container, recommendation string) {
		for _, resource := range allContainerResourceNames {
			l := baseLabelsFn()
			l[labelContainer] = container
			l[labelRecommendation] = recommendation
			l[labelResource] = string(resource)

			gv.Delete(l)
		}
	}

	for _, c := range containers {
		for _, reco := range []string{recoTarget /* To be enabled later, recoLowerBound, recoUpperBound, recoUncappedTarget*/} {
			deleteReco(c, reco)
		}
	}
}

func (r *HvpaReconciler) getContainers(target runtime.Object) ([]string, error) {
	var template v1.PodTemplateSpec
	switch t := target.(type) {
	case *appsv1.Deployment:
		template = t.Spec.Template
	case *appsv1.StatefulSet:
		template = t.Spec.Template
	case *appsv1.ReplicaSet:
		template = t.Spec.Template
	case *appsv1.DaemonSet:
		template = t.Spec.Template
	case *v1.ReplicationController:
		template = *t.Spec.Template
	default:
		return nil, fmt.Errorf("Unhandled target type %T", t)
	}

	n := len(template.Spec.Containers)
	containers := make([]string, n, n)
	for i := range template.Spec.Containers {
		containers[i] = template.Spec.Containers[i].Name
	}

	return containers, nil
}

func (r *HvpaReconciler) deleteScalingMetrics(hvpa *hvpav1alpha1.Hvpa, target runtime.Object) error {
	if hvpa == nil || hvpa.Spec.TargetRef == nil {
		log.V(3).Info("Invalid HVPA resource", "hvpa", hvpa)
		return nil
	}

	var m = r.metrics

	if m.specReplicas != nil {
		m.specReplicas.Delete(basicDetailedLabels(hvpa))
	}

	if m.statusReplicas != nil && hvpa.Status.Replicas != nil {
		m.statusReplicas.Delete(basicDetailedLabels(hvpa))
	}

	if r.EnableDetailedMetrics {
		if m.statusAppliedHPACurrentReplicas != nil {
			m.statusAppliedHPACurrentReplicas.Delete(basicDetailedLabels(hvpa))
		}
		if m.statusAppliedHPADesiredReplicas != nil {
			m.statusAppliedHPADesiredReplicas.Delete(basicDetailedLabels(hvpa))
		}

		containers, err := r.getContainers(target)
		if err != nil {
			log.V(4).Info(err.Error())
			return err
		}

		deleteVPARecommendations(m.statusAppliedVPARecommendation, containers, func() prometheus.Labels {
			return basicDetailedLabels(hvpa)
		})

		for _, reason := range hvpav1alpha1.BlockingReasons {
			if m.statusBlockedHPACurrentReplicas != nil {
				m.statusBlockedHPACurrentReplicas.Delete(basicBlockedLabels(hvpa, reason))
			}
			if m.statusBlockedHPADesiredReplicas != nil {
				m.statusBlockedHPADesiredReplicas.Delete(basicBlockedLabels(hvpa, reason))
			}
			deleteVPARecommendations(m.statusBlockedVPARecommendation, containers, func() prometheus.Labels {
				return basicBlockedLabels(hvpa, reason)
			})
		}
	}

	return nil
}

func (r *HvpaReconciler) updateScalingMetrics(hvpa *hvpav1alpha1.Hvpa, hpaScaled, vpaScaled bool, target runtime.Object) error {
	if hvpa == nil || hvpa.Spec.TargetRef == nil {
		log.V(3).Info("Invalid HVPA resource", "hvpa", hvpa)
		return nil
	}

	var m = r.metrics

	if m.specReplicas != nil {
		if hvpa.Spec.Replicas != nil {
			m.specReplicas.With(basicDetailedLabels(hvpa)).Set(float64(*hvpa.Spec.Replicas))
		} else {
			m.specReplicas.With(basicDetailedLabels(hvpa)).Set(math.NaN())
		}
	}

	if m.statusReplicas != nil {
		if hvpa.Status.Replicas != nil {
			m.statusReplicas.With(basicDetailedLabels(hvpa)).Set(float64(*hvpa.Status.Replicas))
		} else {
			m.statusReplicas.With(basicDetailedLabels(hvpa)).Set(math.NaN())
		}
	}

	if hpaScaled || vpaScaled {
		if m.aggrAppliedScalingsTotal != nil {
			m.aggrAppliedScalingsTotal.With(nil).Inc()
		}
	}

	if m.aggrBlockedScalingsTotal != nil && (!hpaScaled || !vpaScaled) {
		for _, blocked := range hvpa.Status.LastBlockedScaling {
			m.aggrBlockedScalingsTotal.WithLabelValues(string(blocked.Reason)).Inc()
		}
	}

	if r.EnableDetailedMetrics {
		if m.statusAppliedHPACurrentReplicas != nil {
			m.statusAppliedHPACurrentReplicas.With(basicDetailedLabels(hvpa)).Set(float64(hvpa.Status.LastScaling.HpaStatus.CurrentReplicas))
		}
		if m.statusAppliedHPADesiredReplicas != nil {
			m.statusAppliedHPADesiredReplicas.With(basicDetailedLabels(hvpa)).Set(float64(hvpa.Status.LastScaling.HpaStatus.DesiredReplicas))
		}

		containers, err := r.getContainers(target)
		if err != nil {
			log.V(4).Info(err.Error())
			return err
		}

		if hvpa.Status.LastScaling.VpaStatus.Recommendation != nil {
			updateVPARecommendations(m.statusAppliedVPARecommendation, containers, &hvpa.Status.LastScaling.VpaStatus, func() prometheus.Labels {
				return basicDetailedLabels(hvpa)
			})
		} else {
			updateVPARecommendations(m.statusAppliedVPARecommendation, containers, &vpa_api.VerticalPodAutoscalerStatus{
				Recommendation: &vpa_api.RecommendedPodResources{},
			}, func() prometheus.Labels {
				return basicDetailedLabels(hvpa)
			})
		}

		blockedMap := make(map[hvpav1alpha1.BlockingReason]*hvpav1alpha1.BlockedScaling)
		for _, blocked := range hvpa.Status.LastBlockedScaling {
			if blocked == nil {
				log.V(4).Info("Invalid blocked scaling entry", "hvpa", hvpa)
				continue
			}
			blockedMap[blocked.Reason] = blocked
		}
		for _, blockingReason := range hvpav1alpha1.BlockingReasons {
			blocked, ok := blockedMap[blockingReason]
			if m.statusBlockedHPACurrentReplicas != nil {
				if ok {
					m.statusBlockedHPACurrentReplicas.With(basicBlockedLabels(hvpa, blockingReason)).Set(float64(blocked.HpaStatus.CurrentReplicas))
				} else {
					m.statusBlockedHPACurrentReplicas.With(basicBlockedLabels(hvpa, blockingReason)).Set(math.NaN())
				}
			}
			if m.statusBlockedHPADesiredReplicas != nil {
				if ok {
					m.statusBlockedHPADesiredReplicas.With(basicBlockedLabels(hvpa, blockingReason)).Set(float64(blocked.HpaStatus.DesiredReplicas))
				} else {
					m.statusBlockedHPADesiredReplicas.With(basicBlockedLabels(hvpa, blockingReason)).Set(math.NaN())
				}
			}
			if m.statusBlockedVPARecommendation != nil {
				if ok && blocked.VpaStatus.Recommendation != nil {
					updateVPARecommendations(m.statusBlockedVPARecommendation, containers, &blocked.VpaStatus, func() prometheus.Labels {
						return basicBlockedLabels(hvpa, blockingReason)
					})
				} else {
					updateVPARecommendations(m.statusBlockedVPARecommendation, containers, &vpa_api.VerticalPodAutoscalerStatus{
						Recommendation: &vpa_api.RecommendedPodResources{},
					}, func() prometheus.Labels {
						return basicBlockedLabels(hvpa, blockingReason)
					})
				}
			}
		}
	}

	return nil
}
