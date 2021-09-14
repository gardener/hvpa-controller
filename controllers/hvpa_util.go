/*
Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved.

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
	"errors"
	"time"

	hvpav1alpha2 "github.com/gardener/hvpa-controller/api/v1alpha2"
	"github.com/gardener/hvpa-controller/utils"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// adjustForBaseUsage adjusts the new per replica recommendations to account for base usage.
func adjustForBaseUsage(newPerReplica, currentPerReplica corev1.ResourceList, finalReplica, currentReplicas int32, hvpa *hvpav1alpha2.Hvpa) corev1.ResourceList {
	baseUsageSpec := hvpa.Spec.BaseResourcesPerReplica
	if baseUsageSpec == nil || finalReplica == currentReplicas {
		return newPerReplica
	}

	for resourceName, params := range baseUsageSpec {
		var (
			currReq, currReqOk = currentPerReplica[resourceName]
			newReq, newReqOk   = newPerReplica[resourceName]
		)

		if !(currReqOk && !newReqOk) {
			log.V(3).Info("WARNING:", "unsupported resource in BaseResourcesPerReplica", resourceName, "hvpa", client.ObjectKeyFromObject(hvpa))
			continue
		}

		var (
			s        = getScaleFor(resourceName)
			currBase = func() resource.Quantity {
				var q = getChangeQuantity(&params, currReq, s, MaxInt64)
				q.SetScaled(q.ScaledValue(s)*int64(currentReplicas), s)
				return q
			}()
			newBase = func() resource.Quantity {
				var q = getChangeQuantity(&params, newReq, s, MaxInt64)
				q.SetScaled(q.ScaledValue(s)*int64(finalReplica), s)
				return q
			}()
		)

		if currBase.Cmp(*NewQuantity(currReq.Format, func(q *resource.Quantity) {
			q.SetScaled(currReq.ScaledValue(s)*int64(currentReplicas), s)
		})) > 0 {
			// This could happen if scaling is happening for the first time - current request might be set very small
			continue
		}

		// Following statement will update newCPUPerReplica/newMemPerReplica value
		newPerReplica[resourceName] = *NewQuantity(currReq.Format, func(q *resource.Quantity) {
			q.SetScaled((currReq.ScaledValue(s)*int64(finalReplica)-currBase.ScaledValue(s)+newBase.ScaledValue(s))/int64(finalReplica), s)
		})
	}
	return newPerReplica
}

func getMinAllowed(vpaPolicies []vpa_api.ContainerResourcePolicy, containerName string) corev1.ResourceList {
	for i := range vpaPolicies {
		policy := &vpaPolicies[i]
		if policy.ContainerName == containerName {
			if policy.MinAllowed != nil {
				return policy.MinAllowed.DeepCopy()
			}
			return nil
		}
	}
	return nil
}

func finaliseScalingParameters(
	podSpec *corev1.PodSpec,
	currentReplicas, finalReplica, desiredReplicas int32,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *hvpav1alpha2.Hvpa,
	blockedScalingReason map[string]hvpav1alpha2.BlockingReason,
) (
	newPodSpec *corev1.PodSpec,
	desiredVpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	blockedScalingArray []*hvpav1alpha2.BlockedScaling,
	resourceChanged bool, err error,
) {
	newPodSpec = podSpec.DeepCopy()
	// The blockedScalingReason is populated with the reasons for blocking scaling with the following priority order:
	// UpdatePolicy > StabilizationWindow > MaintenanceWindow > MinChanged > ParadoxicalScaling
	var blockedScalingUpdatePolicy, blockedScalingMaintenanceWindow, blockedScalingStabilizationWindow, blockedScalingMinChange, blockedScalingParadoxicalScaling *hvpav1alpha2.BlockedScaling
	var blockedScaling []*hvpav1alpha2.BlockedScaling

	desiredVpaStatus = &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: make([]vpa_api.RecommendedContainerResources, 0, 0),
		},
	}
	limitScalingParams := hvpa.Spec.Vpa.LimitsRequestsGapScaleParams

	for _, reco := range vpaStatus.Recommendation.ContainerRecommendations {
		for i := range newPodSpec.Containers {
			container := &newPodSpec.Containers[i]
			if container.Name == reco.ContainerName {
				var (
					currentPerReplica    = container.Resources.Requests.DeepCopy()
					currentTotal         = corev1.ResourceList{}
					newTotalReco         = corev1.ResourceList{}
					newPerReplica        = corev1.ResourceList{}
					minAllowed           corev1.ResourceList
					isScaleUp            = true
					isParadoxicalScaling = true
				)

				if hvpa.Spec.Vpa.Template.Spec.ResourcePolicy != nil {
					minAllowed = getMinAllowed(hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies, container.Name)
				}

				iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
					var s = getScaleFor(name)

					currentTotal[name] = *NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(currentPerReplica.Name(name, format).ScaledValue(s)*int64(currentReplicas), s)
					})

					newTotalReco[name] = *NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(MaxInt64(
							reco.Target.Name(name, format).ScaledValue(s)*int64(currentReplicas),
							currentPerReplica.Name(name, format).ScaledValue(s)*int64(desiredReplicas),
						), s)
					})

					newPerReplica[name] = *MaxQuantity(
						NewQuantity(format, func(q *resource.Quantity) {
							q.SetScaled(newTotalReco.Name(name, format).ScaledValue(s)/int64(finalReplica), s)
						}),
						minAllowed.Name(name, format),
					)

					// If either or both of memory and cpu are scaling down, consider it as a overall scale down
					isScaleUp = isScaleUp && (newTotalReco.Name(name, format).Cmp(currentTotal.Name(name, format).DeepCopy()) >= 0)

					isParadoxicalScaling = isParadoxicalScaling && (currentPerReplica.Name(name, format).Cmp(newPerReplica.Name(name, format).DeepCopy()) > 0)
				})

				newPerReplica = adjustForBaseUsage(newPerReplica, currentPerReplica, finalReplica, currentReplicas, hvpa)

				// Consider paradoxical only if it's a scale up
				if isScaleUp && finalReplica > currentReplicas {
					if isParadoxicalScaling {
						log.V(2).Info("Paradoxical scaling: current resources recommended because recommendation for horizontal scale is \"scale out\", while for vertical scale is \"scale down\"", "hvpa", client.ObjectKeyFromObject(hvpa))
						break
					}

					iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
						newPerReplica[name] = MaxQuantity(currentPerReplica.Name(name, format), newPerReplica.Name(name, format)).DeepCopy()
					})
				}

				target := newPerReplica.DeepCopy()

				if reason, ok := blockedScalingReason[container.Name]; ok {
					// scaling blocked for this container
					appendToBlockedScaling(&blockedScalingUpdatePolicy, hvpav1alpha2.BlockingReasonUpdatePolicy, target, container.Name, reason == hvpav1alpha2.BlockingReasonUpdatePolicy)
					appendToBlockedScaling(&blockedScalingStabilizationWindow, hvpav1alpha2.BlockingReasonStabilizationWindow, target, container.Name, reason == hvpav1alpha2.BlockingReasonStabilizationWindow)
					appendToBlockedScaling(&blockedScalingMaintenanceWindow, hvpav1alpha2.BlockingReasonMaintenanceWindow, target, container.Name, reason == hvpav1alpha2.BlockingReasonMaintenanceWindow)
					appendToBlockedScaling(&blockedScalingMinChange, hvpav1alpha2.BlockingReasonMinChange, target, container.Name, reason == hvpav1alpha2.BlockingReasonMinChange)
					appendToBlockedScaling(&blockedScalingParadoxicalScaling, hvpav1alpha2.BlockingReasonParadoxicalScaling, target, container.Name, reason == hvpav1alpha2.BlockingReasonParadoxicalScaling)
				} else {
					resourceChanged = true
					container.Resources.Requests = target
					container.Resources.Limits = getScaledLimits(container.Resources.Limits, container.Resources.Requests, target, limitScalingParams)

					desiredVpaStatus.Recommendation.ContainerRecommendations = append(desiredVpaStatus.Recommendation.ContainerRecommendations,
						vpa_api.RecommendedContainerResources{
							ContainerName: container.Name,
							Target:        target,
						})
				}
				break
			}
		}
	}

	if resourceChanged {
		// If we are changing resources of some containers anyway, ignore minChange and stabilisationWindow for other containers
		for _, blockedScalingPtr := range []**hvpav1alpha2.BlockedScaling{&blockedScalingMinChange, &blockedScalingMaintenanceWindow} {
			if *blockedScalingPtr != nil {
				for i := range (*blockedScalingPtr).VpaStatus.ContainerResources {
					containerResources := &(*blockedScalingPtr).VpaStatus.ContainerResources[i]
					target := containerResources.Resources.Requests.DeepCopy()
					desiredVpaStatus.Recommendation.ContainerRecommendations = append(
						desiredVpaStatus.Recommendation.ContainerRecommendations,
						vpa_api.RecommendedContainerResources{
							ContainerName: containerResources.ContainerName,
							Target:        target,
						},
					)
					for j := range newPodSpec.Containers {
						container := &newPodSpec.Containers[j]
						if container.Name == containerResources.ContainerName {
							container.Resources.Requests = target
							container.Resources.Limits = getScaledLimits(container.Resources.Limits, container.Resources.Requests, target, limitScalingParams)
							break
						}
					}
				}
				*blockedScalingPtr = nil
			}
		}
	}

	if blockedScalingUpdatePolicy != nil {
		blockedScaling = append(blockedScaling, blockedScalingUpdatePolicy)
	}
	if blockedScalingStabilizationWindow != nil {
		blockedScaling = append(blockedScaling, blockedScalingStabilizationWindow)
	}
	if blockedScalingMaintenanceWindow != nil {
		blockedScaling = append(blockedScaling, blockedScalingMaintenanceWindow)
	}
	if blockedScalingMinChange != nil {
		blockedScaling = append(blockedScaling, blockedScalingMinChange)
	}
	if blockedScalingParadoxicalScaling != nil {
		blockedScaling = append(blockedScaling, blockedScalingParadoxicalScaling)
	}
	return newPodSpec, desiredVpaStatus, blockedScaling, resourceChanged, nil
}

func isUpdateMode(updateMode *string, expectedMode string) bool {
	if updateMode == nil {
		return false
	}

	return *updateMode == expectedMode
}

func estimateScalingParameters(
	currentReplicas, desiredReplicas int32,
	podSpec *corev1.PodSpec,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *hvpav1alpha2.Hvpa,
	containerBuckets map[string]EffectiveScalingIntervals,
) (
	finalReplicas int32,
	blockedScalingArray map[string]hvpav1alpha2.BlockingReason,
	err error,
) {
	overrideStablization := hvpa.Status.OverrideScaleUpStabilization

	var maintenanceTimeWindow *utils.MaintenanceTimeWindow
	maintenanceWindow := hvpa.Spec.MaintenanceTimeWindow
	if maintenanceWindow != nil {
		maintenanceTimeWindow, err = utils.ParseMaintenanceTimeWindow(maintenanceWindow.Begin, maintenanceWindow.End)
		if err != nil {
			return 0, nil, err
		}
	}

	// Check if scale up and scale down stabilisation windows has passed
	scaleUpStabilizationWindow, scaleDownStabilizationWindow := time.Duration(0), time.Duration(0)
	if hvpa.Spec.ScaleUp.StabilizationDuration != nil {
		scaleUpStabilizationWindow, _ = time.ParseDuration(*hvpa.Spec.ScaleUp.StabilizationDuration)
	}
	if hvpa.Spec.ScaleDown.StabilizationDuration != nil {
		scaleDownStabilizationWindow, _ = time.ParseDuration(*hvpa.Spec.ScaleDown.StabilizationDuration)
	}

	var lastScaleTime *metav1.Time
	if hvpa.Status.LastScaling.LastUpdated != nil {
		lastScaleTime = hvpa.Status.LastScaling.LastUpdated.DeepCopy()
	}
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := metav1.Now().Sub(lastScaleTime.Time)

	var finalReplica int32

	blockedScalingReason := make(map[string]hvpav1alpha2.BlockingReason, 0)

	for _, reco := range vpaStatus.Recommendation.ContainerRecommendations {
		for _, container := range podSpec.Containers {
			if container.Name == reco.ContainerName {
				var (
					currentPerReplicaResources = container.Resources.Requests.DeepCopy()
					currentTotalResources      = corev1.ResourceList{}
					newTotalReco               = corev1.ResourceList{}

					// If either or both of memory and cpu are scaling up, consider it as a overall scale up - erring on the side of stability
					isNoScaling = true
					isScaleDown bool
					isScaleUp   = false

					withinScaleDownMinChange = true
					withinScaleUpMinChange   = true
				)

				iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
					var s = getScaleFor(name)

					currentTotalResources[name] = *NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(currentPerReplicaResources.Name(name, format).ScaledValue(s)*int64(currentReplicas), s)
					})

					newTotalReco[name] = *NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(MaxInt64(
							reco.Target.Name(name, format).ScaledValue(s)*int64(currentReplicas),
							currentPerReplicaResources.Name(name, format).ScaledValue(s)*int64(desiredReplicas),
						), s)
					})

					isNoScaling = isNoScaling && newTotalReco.Name(name, format).Cmp(currentTotalResources.Name(name, format).DeepCopy()) == 0
					isScaleUp = isScaleUp || newTotalReco.Name(name, format).Cmp(currentTotalResources.Name(name, format).DeepCopy()) > 0

					scaleDownMinDelta := getThreshold(NameScaleParams(hvpa.Spec.ScaleDown.MinChange, name), name, currentTotalResources.Name(name, format))
					withinScaleDownMinChange = withinScaleDownMinChange && scaleDownMinDelta.Cmp(*NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(currentTotalResources.Name(name, format).ScaledValue(s)-newTotalReco.Name(name, format).ScaledValue(s), s)
					})) > 0

					scaleUpMinDelta := getThreshold(NameScaleParams(hvpa.Spec.ScaleUp.MinChange, name), name, currentTotalResources.Name(name, format))
					withinScaleUpMinChange = withinScaleUpMinChange && scaleUpMinDelta.Cmp(*NewQuantity(format, func(q *resource.Quantity) {
						q.SetScaled(newTotalReco.Name(name, format).ScaledValue(s)-currentTotalResources.Name(name, format).ScaledValue(s), s)
					})) > 0
				})

				isScaleDown = !isScaleUp

				if isNoScaling {
					break
				}

				if (isScaleUp && isUpdateMode(hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode, hvpav1alpha2.UpdateModeOff)) ||
					(isScaleDown && isUpdateMode(hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode, hvpav1alpha2.UpdateModeOff)) {
					blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonUpdatePolicy
					break
				}

				if (!overrideStablization && isScaleUp && scaleUpStabilizationWindow > lastScaleTimeDuration) ||
					(isScaleDown && scaleDownStabilizationWindow > lastScaleTimeDuration) {
					blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonStabilizationWindow
					break
				}

				// Check for maintenanceWindow mode
				if (isScaleDown && isUpdateMode(hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode, hvpav1alpha2.UpdateModeMaintenanceWindow)) ||
					(isScaleUp && isUpdateMode(hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode, hvpav1alpha2.UpdateModeMaintenanceWindow)) {
					if maintenanceTimeWindow == nil {
						return 0, nil, errors.New("scale up/down update mode is maintenanceWindow but maintenance time window is not provided")
					}
					if !maintenanceTimeWindow.Contains(time.Now()) {
						blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonMaintenanceWindow
						break
					}
				}

				if isScaleDown {
					// Scale Down scenario only if none of the resources is scaling up. Check for minChange
					if withinScaleDownMinChange {
						blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonMinChange
						break
					}
				} else {
					// Scale Up. Check for minChange
					if !overrideStablization && withinScaleUpMinChange {
						blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonMinChange
						break
					}
				}

				var (
					maxReplica                       int32
					atLeastOneMatchesCurrentReplicas bool
				)

				iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
					var r, err = containerBuckets[container.Name].GetReplicasForResource(name, newTotalReco.Name(name, format).DeepCopy(), currentReplicas)
					if err == ErrorOutOfRange {
						// This can happen when VPA's maxAllowed is more than maxCPU set in the last scaleInterval
						// Use maxReplicas
						max := NameScaleInterval(hvpa.Spec.ScaleIntervals[len(hvpa.Spec.ScaleIntervals)-1], name, format)
						if max != nil && reco.Target.Name(name, format).Cmp(max.DeepCopy()) > 0 {
							r = hvpa.Spec.ScaleIntervals[len(hvpa.Spec.ScaleIntervals)-1].MaxReplicas
						}
					}

					maxReplica = MaxInt32(maxReplica, r)
					atLeastOneMatchesCurrentReplicas = atLeastOneMatchesCurrentReplicas || r == currentReplicas
				})

				if maxReplica == currentReplicas {
					// no need to change bucket since recommended cpu AND memory fall in the same bucket
					finalReplica = MaxInt32(finalReplica, currentReplicas)
					break
				} else if isScaleDown && atLeastOneMatchesCurrentReplicas {
					// scaling down - no need to change bucket since recommended cpu OR memory fall in the same bucket
					// This provides hysteresis while scaling down.
					finalReplica = MaxInt32(finalReplica, currentReplicas)
					break
				}

				var paradoxicalScaling = true
				iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
					var (
						newPerReplicaQ = *NewQuantity(format, func(q *resource.Quantity) {
							var s = getScaleFor(name)
							q.SetScaled(newTotalReco.Name(name, format).ScaledValue(s)/int64(maxReplica), s)
						})

						currentPerReplicaQ = currentPerReplicaResources.Name(name, format)
					)

					paradoxicalScaling = paradoxicalScaling && !currentPerReplicaQ.IsZero() && currentPerReplicaQ.Cmp(newPerReplicaQ) < 0
					if paradoxicalScaling {
						log.V(2).Info("Paradoxical scaling: No scaling recommended because recommendation for horizontal scale is \"scale in\", while for vertical scale is \"scale up\"", "recommended replica", maxReplica, "resource", name, "recommended", newPerReplicaQ, "hvpa", client.ObjectKeyFromObject(hvpa))
					}
				})

				// Block paradoxical scaling only if it is a scale down
				if isScaleDown && maxReplica < currentReplicas {
					if paradoxicalScaling {
						// Block scaling for this container
						blockedScalingReason[container.Name] = hvpav1alpha2.BlockingReasonParadoxicalScaling
						break
					}
				}

				finalReplica = MaxInt32(finalReplica, maxReplica)
				break
			}
		}
	}
	if finalReplica == 0 {
		// scaling for all the containers is blocked
		finalReplica = currentReplicas
	}
	return finalReplica, blockedScalingReason, nil
}

func getScalingRecommendations(
	hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *hvpav1alpha2.Hvpa,
	podSpec *corev1.PodSpec,
	currentReplicas int32,
) (
	scaledStatus *hvpav1alpha2.ScalingStatus,
	newPodSpec *corev1.PodSpec,
	resourceChanged bool,
	blockingReason *[]*hvpav1alpha2.BlockedScaling,
	err error,
) {
	if vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.V(3).Info("VPA is not ready yet. Will not scale", "hvpa", client.ObjectKeyFromObject(hvpa))
		return nil, nil, false, nil, nil
	}

	if !vpaStatusConditionsSupported(vpaStatus, client.ObjectKeyFromObject(hvpa).String()) {
		return nil, nil, false, nil, errors.New("VPA status conditions not supported")
	}

	// Initialise desiredReplicas to 0.
	// Otherwise it might prevent scale down if HPA is not deployed - because we take max of VPA's and HPA's total recommendations
	desiredReplicas := int32(0)
	if hpaStatus != nil {
		desiredReplicas = hpaStatus.DesiredReplicas
	}

	containerBuckets, err := GetEffectiveScalingIntervals(hvpa, currentReplicas)
	if err != nil {
		return nil, nil, false, nil, err
	}
	if containerBuckets == nil {
		log.V(3).Info("hvpa", "No scaling because buckets were not generated", "hvpa", client.ObjectKeyFromObject(hvpa))
		return nil, nil, false, nil, nil
	}

	finalReplica, blockedScalingReason, err := estimateScalingParameters(currentReplicas, desiredReplicas, podSpec, vpaStatus, hvpa, containerBuckets)
	if err != nil {
		return nil, nil, false, nil, err
	}
	if finalReplica == 0 {
		return nil, nil, false, nil, nil
	}

	newPodSpec, desiredVpaStatus, blockedScaling, resourceChanged, err := finaliseScalingParameters(podSpec, currentReplicas, finalReplica, desiredReplicas, vpaStatus, hvpa, blockedScalingReason)
	if err != nil {
		return nil, nil, false, nil, err
	}

	desiredHpaStatus := &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: finalReplica,
	}
	if !resourceChanged {
		newPodSpec = nil
	}
	if resourceChanged || finalReplica != currentReplicas {
		scaledStatus = getScalingStatusFrom(desiredHpaStatus, desiredVpaStatus, newPodSpec)
	}

	return scaledStatus, newPodSpec, resourceChanged, &blockedScaling, nil
}

func newBlockedScaling(reason hvpav1alpha2.BlockingReason) *hvpav1alpha2.BlockedScaling {
	blockedScaling := hvpav1alpha2.BlockedScaling{
		Reason: reason,
		ScalingStatus: hvpav1alpha2.ScalingStatus{
			VpaStatus: hvpav1alpha2.VpaStatus{
				ContainerResources: make([]hvpav1alpha2.ContainerResources, 0, 0),
			},
		},
	}

	return &blockedScaling
}

func appendToBlockedScaling(blockedScaling **hvpav1alpha2.BlockedScaling, reason hvpav1alpha2.BlockingReason, target corev1.ResourceList, container string, blocked bool) {
	if blocked {
		if *blockedScaling == nil {
			*blockedScaling = newBlockedScaling(reason)
		}
		(*blockedScaling).VpaStatus.ContainerResources = append(
			(*blockedScaling).VpaStatus.ContainerResources,
			hvpav1alpha2.ContainerResources{
				Resources: corev1.ResourceRequirements{
					Requests: target,
				},
				ContainerName: container,
			})
	}
}

func vpaStatusConditionsSupported(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpaKey string) bool {
	expectedVpaConditionsStatus := map[vpa_api.VerticalPodAutoscalerConditionType]corev1.ConditionStatus{
		vpa_api.ConfigUnsupported:      corev1.ConditionFalse,
		vpa_api.ConfigDeprecated:       corev1.ConditionFalse,
		vpa_api.LowConfidence:          corev1.ConditionFalse,
		vpa_api.RecommendationProvided: corev1.ConditionTrue,
	}
	recommended := false

	for _, v := range vpaStatus.Conditions {
		if expected, known := expectedVpaConditionsStatus[v.Type]; known && expected != v.Status {
			log.V(3).Info("VPA", "VPA status condition", v.Type, "Expected", expected, "Found", v.Status, "For HVPA", hvpaKey)
			return false
		}
		if v.Type == vpa_api.RecommendationProvided {
			recommended = true
		}
	}
	return recommended
}
