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
	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace          = "hvpa"
	metricsSubsystemAggregate = "aggregate"
	metricsSubsystemSpec      = "spec"
	metricsSubsystemStatus    = "status"
	labelNamespace            = "namespace"
	labelName                 = "name"
	labelReason               = "reason"
	labelHpaUpdatePolicy      = "hpaUpdatePolicy"
	labelVpaUpdatePolicy      = "vpaUpdatePolicy"
	labelTargetRefName        = "targetName"
	labelTargetRefKind        = "targetKind"
	labelContainer            = "container"
	labelResource             = "resource"
	labelRecommendation       = "recommendation"
	recoTarget                = "target"
	recoLowerBound            = "lowerBound"
	recoUpperBound            = "upperBound"
	recoUncappedTarget        = "uncappedTarget"
)

type hvpaMetrics struct {
	aggrResourcesTotal              *prometheus.GaugeVec
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
	m.aggrResourcesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystemAggregate,
			Name:      "resources_total",
			Help:      "The number of HVPA resources currenrly managed by the HVPA controller.",
		},
		nil,
	)
	allCollectors = append(allCollectors, m.aggrResourcesTotal)

	m.aggrAppliedScalingsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystemAggregate,
			Name:      "applied_scaling_total",
			Help:      "The number of scalings applied by the HVPA controller.",
		},
		nil,
	)
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
	allCollectors = append(allCollectors, m.aggrBlockedScalingsTotal)

	if r.EnableDetailedMetrics {
		m.specReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemSpec,
				Name:      "replicas",
				Help:      "The number of replicas in the HVPA spec (part of the Scale sub-resource).",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.specReplicas)

		m.statusReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The number of replicas in the HVPA status (part of the Scale sub-resource).",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusReplicas)

		m.statusAppliedHPACurrentReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the applied current replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusAppliedHPACurrentReplicas)

		m.statusAppliedHPADesiredReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the applied desired replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy},
		)
		allCollectors = append(allCollectors, m.statusAppliedHPADesiredReplicas)

		m.statusAppliedVPARecommendation = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the applied recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy, labelContainer, labelRecommendation, labelResource},
		)
		allCollectors = append(allCollectors, m.statusAppliedVPARecommendation)

		m.statusBlockedHPACurrentReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the blocked current replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy, labelReason},
		)
		allCollectors = append(allCollectors, m.statusBlockedHPACurrentReplicas)

		m.statusBlockedHPADesiredReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the blocked desired replicas recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy, labelReason},
		)
		allCollectors = append(allCollectors, m.statusBlockedHPADesiredReplicas)

		m.statusBlockedVPARecommendation = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: metricsSubsystemStatus,
				Name:      "replicas",
				Help:      "The the ap, labelReasonplied recommendation from HPA.",
			},
			[]string{labelNamespace, labelName, labelTargetRefKind, labelTargetRefName, labelHpaUpdatePolicy, labelVpaUpdatePolicy, labelReason, labelContainer, labelRecommendation, labelResource},
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

func (r *HvpaReconciler) incrementResourcesTotal() {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.aggrResourcesTotal.With(nil).Inc()
}

func (r *HvpaReconciler) decrementResourcesTotal() {
	if r == nil || r.metrics == nil {
		return
	}

	r.metrics.aggrResourcesTotal.With(nil).Dec()
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
	if hvpa.Spec.Hpa.UpdatePolicy != nil && hvpa.Spec.Hpa.UpdatePolicy.UpdateMode != nil {
		l[labelHpaUpdatePolicy] = *hvpa.Spec.Hpa.UpdatePolicy.UpdateMode
	}
	if hvpa.Spec.Vpa.UpdatePolicy != nil && hvpa.Spec.Vpa.UpdatePolicy.UpdateMode != nil {
		l[labelVpaUpdatePolicy] = *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode
	}
	return l
}

func basicBlockedLabels(hvpa *hvpav1alpha1.Hvpa, reason hvpav1alpha1.BlockingReason) prometheus.Labels {
	l := basicDetailedLabels(hvpa)
	l[labelReason] = string(reason)
	return l
}

func addVPARecommendations(gv *prometheus.GaugeVec, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, baseLabelsFn func() prometheus.Labels) {
	if gv == nil || vpaStatus == nil || vpaStatus.Recommendation == nil {
		return
	}

	addReco := func(container, recommendation string, resourceList v1.ResourceList) {
		for resource, q := range resourceList {
			l := baseLabelsFn()
			l[labelContainer] = container
			l[labelRecommendation] = recommendation
			l[labelResource] = string(resource)

			if resource == "cpu" {
				gv.With(l).Set(float64(q.MilliValue()))
			} else {
				gv.With(l).Set(float64(q.Value()))
			}
		}
	}

	for i := range vpaStatus.Recommendation.ContainerRecommendations {
		cr := &vpaStatus.Recommendation.ContainerRecommendations[i]
		addReco(cr.ContainerName, recoTarget, cr.Target)
		addReco(cr.ContainerName, recoLowerBound, cr.LowerBound)
		addReco(cr.ContainerName, recoUpperBound, cr.UpperBound)
		addReco(cr.ContainerName, recoUncappedTarget, cr.UncappedTarget)
	}
}

func (r *HvpaReconciler) updateScalingMetrics(hvpa *hvpav1alpha1.Hvpa, hpaScaled, vpaScaled bool) {
	if hvpa == nil || hvpa.Spec.TargetRef == nil {
		log.V(3).Info("Invalid HVPA resource", "hvpa", hvpa)
		return
	}

	var m = r.metrics

	if m.specReplicas != nil && hvpa.Spec.Replicas != nil {
		m.specReplicas.With(basicDetailedLabels(hvpa)).Set(float64(*hvpa.Spec.Replicas))
	}

	if m.statusReplicas != nil && hvpa.Status.Replicas != nil {
		m.statusReplicas.With(basicDetailedLabels(hvpa)).Set(float64(*hvpa.Status.Replicas))
	}

	if hpaScaled || vpaScaled {
		if m.aggrAppliedScalingsTotal != nil {
			m.aggrAppliedScalingsTotal.With(nil).Inc()
		}
	}

	if r.EnableDetailedMetrics && hpaScaled {
		if m.statusAppliedHPACurrentReplicas != nil {
			m.statusAppliedHPACurrentReplicas.With(basicDetailedLabels(hvpa)).Set(float64(hvpa.Status.LastScaling.HpaStatus.CurrentReplicas))
		}
		if m.statusAppliedHPADesiredReplicas != nil {
			m.statusAppliedHPADesiredReplicas.With(basicDetailedLabels(hvpa)).Set(float64(hvpa.Status.LastScaling.HpaStatus.DesiredReplicas))
		}
	}

	if r.EnableDetailedMetrics && vpaScaled {
		addVPARecommendations(m.statusAppliedVPARecommendation, &hvpa.Status.LastScaling.VpaStatus, func() prometheus.Labels {
			return basicDetailedLabels(hvpa)
		})
	}

	for _, blocked := range hvpa.Status.LastBlockedScaling {
		if blocked == nil {
			log.V(4).Info("Invalid blocked scaling entry", "hvpa", hvpa)
			continue
		}

		if m.aggrBlockedScalingsTotal != nil {
			m.aggrBlockedScalingsTotal.WithLabelValues(string(blocked.Reason)).Inc()
		}

		if r.EnableDetailedMetrics {
			if m.statusBlockedHPACurrentReplicas != nil {
				m.statusBlockedHPACurrentReplicas.With(basicBlockedLabels(hvpa, blocked.Reason)).Set(float64(blocked.HpaStatus.CurrentReplicas))
			}
			if m.statusBlockedHPADesiredReplicas != nil {
				m.statusBlockedHPADesiredReplicas.With(basicBlockedLabels(hvpa, blocked.Reason)).Set(float64(blocked.HpaStatus.DesiredReplicas))
			}
			addVPARecommendations(m.statusBlockedVPARecommendation, &blocked.VpaStatus, func() prometheus.Labels {
				return basicBlockedLabels(hvpa, blocked.Reason)
			})
		}
	}
}
