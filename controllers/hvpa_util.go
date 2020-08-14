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
	"math"
	"time"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/gardener/hvpa-controller/utils"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

func adjustForBaseUsage(newCPUPerReplica, currentperReplicaCPU, newMemPerReplica, currentperReplicaMem int64, finalReplica, currentReplicas int32, hvpa *hvpav1alpha1.Hvpa) (newCPU, newMem int64) {
	baseUsageSpec := hvpa.Spec.BaseResourcesPerReplica
	if baseUsageSpec == nil || finalReplica == currentReplicas {
		return newCPUPerReplica, newMemPerReplica
	}

	var currReco, currReq *int64

	for resourceName, params := range baseUsageSpec {
		deltaPercReco, deltaPercReq, deltaVal := int64(0), int64(0), int64(0)

		if resourceName == corev1.ResourceCPU {
			currReq = &currentperReplicaCPU
			currReco = &newCPUPerReplica
		} else if resourceName == corev1.ResourceMemory {
			currReq = &currentperReplicaMem
			currReco = &newMemPerReplica
		} else {
			log.V(3).Info("WARNING:", "unsupported resource in BaseResourcesPerReplica", resourceName, "hvpa", hvpa.Namespace+"/"+hvpa.Name)
			continue
		}
		perc := params.Percentage
		value := params.Value
		if perc != nil {
			deltaPercReq = *currReq * int64(*perc) / 100
			deltaPercReco = *currReco * int64(*perc) / 100
		}
		if value != nil {
			valBase := resource.MustParse(*value)
			deltaVal = valBase.MilliValue()
		}
		currBase := int64(math.Max(float64(deltaPercReq), float64(deltaVal))) * int64(currentReplicas)
		if *currReq*int64(currentReplicas) <= currBase {
			// This could happen if scaling is happening for the first time - current request might be set very small
			continue
		}
		newBase := int64(math.Max(float64(deltaPercReco), float64(deltaVal)) * float64(finalReplica))
		// Following statement will update newCPUPerReplica/newMemPerReplica value
		*currReco = (*currReco*int64(finalReplica) - currBase + newBase) / int64(finalReplica)
	}
	return newCPUPerReplica, newMemPerReplica
}

func getScalingRecommendations(
	hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *hvpav1alpha1.Hvpa,
	podSpec *corev1.PodSpec,
	currentReplicas int32,
) (
	scaledStatus *hvpav1alpha1.ScalingStatus,
	newPodSpec *corev1.PodSpec,
	resourceChanged bool,
	blockingReason *[]*hvpav1alpha1.BlockedScaling,
	err error,
) {
	if vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.V(3).Info("VPA is not ready yet. Will not scale", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, nil, false, nil, nil
	}

	if !vpaStatusConditionsSupported(vpaStatus, hvpa.Namespace+"/"+hvpa.Name) {
		return nil, nil, false, nil, errors.New("VPA status conditions not supported")
	}

	overrideStablization := hvpa.Status.OverrideScaleUpStabilization

	var maintenanceTimeWindow *utils.MaintenanceTimeWindow
	maintenanceWindow := hvpa.Spec.MaintenanceTimeWindow
	if maintenanceWindow != nil {
		maintenanceTimeWindow, err = utils.ParseMaintenanceTimeWindow(maintenanceWindow.Begin, maintenanceWindow.End)
		if err != nil {
			return nil, nil, false, nil, err
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

	// Initialise desiredReplicas to 1.
	// Otherwise it might prevent scale down if HPA is not deployed - because we take max of VPA's and HPA's total recommendations
	desiredReplicas := int32(1)
	if hpaStatus != nil {
		desiredReplicas = hpaStatus.DesiredReplicas
	}

	limitScalingParams := hvpa.Spec.Vpa.LimitsRequestsGapScaleParams
	containerBuckets, currentBucket, err := GetBuckets(hvpa, currentReplicas)
	if err != nil {
		return nil, nil, false, nil, err
	}

	var finalReplica int32
	var blockedScalingUpdatePolicy, blockedScalingMaintenanceWindow, blockedScalingStabilizationWindow, blockedScalingMinChange, blockedScalingParadoxicalScaling *hvpav1alpha1.BlockedScaling
	var blockedScaling []*hvpav1alpha1.BlockedScaling

	// The blockedScalingReason is populated with the reasons for blocking scaling with the following priority order:
	// UpdatePolicy > StabilizationWindow > MaintenanceWindow > MinChanged > ParadoxicalScaling
	blockedScalingReason := make(map[string]hvpav1alpha1.BlockingReason, 0)

	desiredVpaStatus := &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: make([]vpa_api.RecommendedContainerResources, 0, 0),
		},
	}

	for _, reco := range vpaStatus.Recommendation.ContainerRecommendations {
		for _, container := range podSpec.Containers {
			if container.Name == reco.ContainerName {
				currentperReplicaCPU := container.Resources.Requests.Cpu().MilliValue()
				currentperReplicaMem := container.Resources.Requests.Memory().MilliValue()

				currentTotalCPU := currentperReplicaCPU * int64(currentReplicas)
				currentTotalMem := currentperReplicaMem * int64(currentReplicas)

				scaleUpMinDeltaMem := getThreshold(&hvpa.Spec.ScaleUp.MinChange.Memory, corev1.ResourceMemory, currentTotalMem)
				scaleDownMinDeltaMem := getThreshold(&hvpa.Spec.ScaleDown.MinChange.Memory, corev1.ResourceMemory, currentTotalMem)
				scaleUpMinDeltaCPU := getThreshold(&hvpa.Spec.ScaleUp.MinChange.CPU, corev1.ResourceCPU, currentTotalCPU)
				scaleDownMinDeltaCPU := getThreshold(&hvpa.Spec.ScaleDown.MinChange.CPU, corev1.ResourceCPU, currentTotalCPU)

				newTotalCPUReco := int64(math.Max(float64(reco.Target.Cpu().MilliValue()*int64(currentReplicas)), float64(currentperReplicaCPU*int64(desiredReplicas))))
				newTotalMemReco := int64(math.Max(float64(reco.Target.Memory().MilliValue()*int64(currentReplicas)), float64(currentperReplicaMem*int64(desiredReplicas))))

				if newTotalCPUReco == currentTotalCPU && newTotalMemReco == currentTotalMem {
					break
				}

				// If either or both of memory and cpu are scaling up, consider it as a overall scale up - erring on the side of stability
				isScaleDown := newTotalCPUReco <= currentTotalCPU && newTotalMemReco <= currentTotalMem
				isScaleUp := !isScaleDown

				if (isScaleUp && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == hvpav1alpha1.UpdateModeOff) ||
					(isScaleDown && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == hvpav1alpha1.UpdateModeOff) {
					blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonUpdatePolicy
					break
				}

				if (!overrideStablization && isScaleUp && scaleUpStabilizationWindow > lastScaleTimeDuration) ||
					(isScaleDown && scaleDownStabilizationWindow > lastScaleTimeDuration) {
					blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonStabilizationWindow
					break
				}

				// Check for maintenanceWindow mode
				if (isScaleDown && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == hvpav1alpha1.UpdateModeMaintenanceWindow) ||
					(isScaleUp && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == hvpav1alpha1.UpdateModeMaintenanceWindow) {
					if maintenanceTimeWindow == nil {
						return nil, nil, false, nil, errors.New("scale up/down update mode is maintenanceWindow but maintenance time window is not provided")
					}
					if !maintenanceTimeWindow.Contains(time.Now()) {
						blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonMaintenanceWindow
						break
					}
				}

				if isScaleDown {
					// Scale Down scenario only if none of the resources is scaling up. Check for minChange
					if currentTotalCPU-newTotalCPUReco < scaleDownMinDeltaCPU &&
						currentTotalMem-newTotalMemReco < scaleDownMinDeltaMem {
						blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonMinChange
						break
					}
				} else {
					// Scale Up. Check for minChange
					if !overrideStablization && newTotalCPUReco-currentTotalCPU < scaleUpMinDeltaCPU &&
						newTotalMemReco-currentTotalMem < scaleUpMinDeltaMem {
						blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonMinChange
						break
					}
				}

				currentBucketHasCPU := currentBucket.HasValue(newTotalCPUReco, ResourceReplicas, ResourceCPU) == 0
				currentBucketHasMem := currentBucket.HasValue(newTotalMemReco, ResourceReplicas, ResourceMemory) == 0

				if currentBucketHasCPU && currentBucketHasMem {
					// no need to change bucket since recommended cpu AND memory fall in the same bucket
					// We can calculate new resource values based on current bucket itself then,
					// no need to use the generic bucket list anymore - overwrite:
					finalReplica = currentReplicas
					break
				} else if isScaleDown && (currentBucketHasCPU || currentBucketHasMem) {
					// scaling down - no need to change bucket since recommended cpu OR memory fall in the same bucket
					// This provides hysteresis while scaling down.
					// We can calculate new resource values based on current bucket itself then,
					// no need to use the generic bucket list anymore - overwrite:
					finalReplica = currentReplicas
					break
				}

				replicaByCPU, _, _ := containerBuckets[container.Name].FindXValue(newTotalCPUReco, ResourceReplicas, ResourceCPU)
				replicaByMem, _, _ := containerBuckets[container.Name].FindXValue(newTotalMemReco, ResourceReplicas, ResourceMemory)
				maxReplica := int32(math.Max(float64(replicaByCPU), float64(replicaByMem)))

				newPerReplicaCPU := newTotalCPUReco / int64(maxReplica)
				newPerReplicaMem := newTotalMemReco / int64(maxReplica)

				if maxReplica < currentReplicas {
					if (currentperReplicaCPU != 0 && currentperReplicaCPU < newPerReplicaCPU) && (currentperReplicaMem != 0 && currentperReplicaMem < newPerReplicaMem) {
						// Paradoxical Scaling - block scaling for this container
						log.V(2).Info("Paradoxical scaling: No scaling recommended because recommendation for horizontal scale is \"scale in\", while for vertical scale is \"scale up\"", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
						blockedScalingReason[container.Name] = hvpav1alpha1.BlockingReasonParadoxicalScaling
						break
					}
				}
				if maxReplica > finalReplica {
					finalReplica = maxReplica
				}
				break
			}
		}
	}
	if finalReplica == 0 {
		// scaling for all the containers is blocked
		finalReplica = currentReplicas
	}

	newPodSpec = podSpec.DeepCopy()

	for _, reco := range vpaStatus.Recommendation.ContainerRecommendations {
		for i := range newPodSpec.Containers {
			container := &newPodSpec.Containers[i]
			if container.Name == reco.ContainerName {
				currentperReplicaCPU := container.Resources.Requests.Cpu().MilliValue()
				currentperReplicaMem := container.Resources.Requests.Memory().MilliValue()

				newTotalCPUReco := int64(math.Max(float64(reco.Target.Cpu().MilliValue()*int64(currentReplicas)), float64(container.Resources.Requests.Cpu().MilliValue()*int64(desiredReplicas))))
				newTotalMemReco := int64(math.Max(float64(reco.Target.Memory().MilliValue()*int64(currentReplicas)), float64(container.Resources.Requests.Memory().MilliValue()*int64(desiredReplicas))))

				newCPUPerReplica := newTotalCPUReco / int64(finalReplica)
				newMemPerReplica := newTotalMemReco / int64(finalReplica)

				newCPUPerReplica, newMemPerReplica = adjustForBaseUsage(newCPUPerReplica, currentperReplicaCPU, newMemPerReplica, currentperReplicaMem, finalReplica, currentReplicas, hvpa)

				if finalReplica > currentReplicas {
					if currentperReplicaCPU > newCPUPerReplica && currentperReplicaMem > newMemPerReplica {
						// Paradoxical Scaling
						log.V(2).Info("Paradoxical scaling: current resources recommended because recommendation for horizontal scale is \"scale out\", while for vertical scale is \"scale down\"", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
						break
					}
					newCPUPerReplica = int64(math.Max(float64(currentperReplicaCPU), float64(newCPUPerReplica)))
					newMemPerReplica = int64(math.Max(float64(currentperReplicaMem), float64(newMemPerReplica)))
				}

				newCPU := resource.NewScaledQuantity(newCPUPerReplica, resource.Milli)
				_ = newCPU.String() // cache string q.s
				newMem := resource.NewScaledQuantity(newMemPerReplica, resource.Milli)
				newMem.SetScaled(newMem.ScaledValue(resource.Kilo), resource.Kilo)

				target := corev1.ResourceList{
					corev1.ResourceCPU:    *newCPU,
					corev1.ResourceMemory: *newMem,
				}
				if reason, ok := blockedScalingReason[container.Name]; ok {
					// scaling blocked for this container
					appendToBlockedScaling(&blockedScalingUpdatePolicy, hvpav1alpha1.BlockingReasonUpdatePolicy, target, container.Name, reason == hvpav1alpha1.BlockingReasonUpdatePolicy)
					appendToBlockedScaling(&blockedScalingStabilizationWindow, hvpav1alpha1.BlockingReasonStabilizationWindow, target, container.Name, reason == hvpav1alpha1.BlockingReasonStabilizationWindow)
					appendToBlockedScaling(&blockedScalingMaintenanceWindow, hvpav1alpha1.BlockingReasonMaintenanceWindow, target, container.Name, reason == hvpav1alpha1.BlockingReasonMaintenanceWindow)
					appendToBlockedScaling(&blockedScalingMinChange, hvpav1alpha1.BlockingReasonMinChange, target, container.Name, reason == hvpav1alpha1.BlockingReasonMinChange)
					appendToBlockedScaling(&blockedScalingParadoxicalScaling, hvpav1alpha1.BlockingReasonParadoxicalScaling, target, container.Name, reason == hvpav1alpha1.BlockingReasonParadoxicalScaling)
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

func newBlockedScaling(reason hvpav1alpha1.BlockingReason) *hvpav1alpha1.BlockedScaling {
	blockedScaling := hvpav1alpha1.BlockedScaling{
		Reason: reason,
		ScalingStatus: hvpav1alpha1.ScalingStatus{
			VpaStatus: hvpav1alpha1.VpaStatus{
				ContainerResources: make([]hvpav1alpha1.ContainerResources, 0, 0),
			},
		},
	}

	return &blockedScaling
}

func appendToBlockedScaling(blockedScaling **hvpav1alpha1.BlockedScaling, reason hvpav1alpha1.BlockingReason, target corev1.ResourceList, container string, blocked bool) {
	if blocked {
		if *blockedScaling == nil {
			*blockedScaling = newBlockedScaling(reason)
		}
		(*blockedScaling).VpaStatus.ContainerResources = append(
			(*blockedScaling).VpaStatus.ContainerResources,
			hvpav1alpha1.ContainerResources{
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
