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

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	"github.com/gardener/hvpa-controller/utils"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

const (
	// ResourceCPU represents CPU in millicores (1core = 1000millicores).
	ResourceCPU utils.AxisName = "cpu"
	// ResourceMemory represents memory, in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024).
	ResourceMemory utils.AxisName = "memory"
	// ResourceReplicas represents replicas.
	ResourceReplicas utils.AxisName = "replicas"
)

func getLinearBucket(scaleInterval *autoscalingv1alpha1.ScaleIntervals, minCPU, minMemory int64, minReplicas int32) (utils.Bucket, error) {
	linearBucket := utils.NewLinearBucket()

	maxReplicas := scaleInterval.MaxReplicas
	delta := int64(1) // Because we iterate over replicas in steps of 1
	err := linearBucket.AddAxis(ResourceReplicas, int64(minReplicas), int64(maxReplicas), delta)
	if err != nil {
		return nil, err
	}

	maxCPU := resource.MustParse(scaleInterval.MaxCPU)
	delta = maxCPU.MilliValue() - minCPU // Because we don't iterate over this axis anyway
	err = linearBucket.AddAxis(ResourceCPU, minCPU, maxCPU.MilliValue(), delta)
	if err != nil {
		return nil, err
	}

	maxMemory := resource.MustParse(scaleInterval.MaxMemory)
	delta = maxMemory.MilliValue() - minMemory // Because we don't iterate over this axis anyway
	err = linearBucket.AddAxis(ResourceMemory, minMemory, maxMemory.MilliValue(), delta)
	if err != nil {
		return nil, err
	}

	return linearBucket, nil
}

// Returns minAllowed of all containers for which VPA mode is not set to "Off"
func getMinAllowed(containerPolicies []vpa_api.ContainerResourcePolicy) (minCPU, minMemory int64) {
	var cpu, memory int64

	for _, policy := range containerPolicies {
		if policy.MinAllowed != nil && (policy.Mode == nil || *policy.Mode != vpa_api.ContainerScalingModeOff) {
			if policy.MinAllowed.Cpu() != nil {
				cpu += policy.MinAllowed.Cpu().MilliValue()
			}
			if policy.MinAllowed.Cpu() != nil {
				memory += policy.MinAllowed.Memory().MilliValue()
			}
		}
	}

	return cpu, memory
}

func getBuckets(hvpa *autoscalingv1alpha1.Hvpa, currentReplicas int32) (bucketList, currentBucket utils.Bucket, err error) {
	var (
		buckets             []utils.Bucket
		bucket              utils.Bucket
		currBucket          utils.Bucket
		deltaPerc, deltaVal int64
		currMin             *int64
	)
	scalingOverlap := hvpa.Spec.ScalingIntervalsOverlap

	minReplicas := *hvpa.Spec.Hpa.Template.Spec.MinReplicas
	minCPU, minMemory := getMinAllowed(hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies)

	for _, scaleInterval := range hvpa.Spec.ScaleIntervals {
		maxCPU := resource.MustParse(scaleInterval.MaxCPU)
		maxMemory := resource.MustParse(scaleInterval.MaxMemory)

		if scaleInterval.EffectiveIntervalExtrapolation != nil && *scaleInterval.EffectiveIntervalExtrapolation == autoscalingv1alpha1.EffectiveIntervalExtrapolicationHorizontalOnly {
			bucket, err = getLinearBucket(&scaleInterval, maxCPU.MilliValue(), maxMemory.MilliValue(), minReplicas)
		} else {
			bucket, err = getLinearBucket(&scaleInterval, minCPU, minMemory, minReplicas)
		}
		if err != nil {
			return nil, nil, err
		}

		if currentReplicas <= scaleInterval.MaxReplicas && currentReplicas >= minReplicas {
			currBucket = bucket
		}

		buckets = append(buckets, bucket)

		// Prepare for next iteration
		minReplicas = scaleInterval.MaxReplicas + 1
		minCPU = maxCPU.MilliValue() * int64(scaleInterval.MaxReplicas) / int64(minReplicas)
		minMemory = maxMemory.MilliValue() * int64(scaleInterval.MaxReplicas) / int64(minReplicas)

		if scalingOverlap != nil {
			// Adjust min values for overlap
			for resourceName, params := range scalingOverlap {
				if resourceName == corev1.ResourceCPU {
					currMin = &minCPU
				} else if resourceName == corev1.ResourceMemory {
					currMin = &minMemory
				} else {
					log.V(3).Info("WARNING:", "unsupported resource in ScalingIntervalsOverlap", resourceName, "hvpa", hvpa.Namespace+"/"+hvpa.Name)
					continue
				}
				perc := params.Percentage
				if perc != nil {
					deltaPerc = *currMin * int64(*perc) / 100
				}
				if params.Value != nil {
					val := resource.MustParse(*params.Value)
					deltaVal = val.MilliValue()
				}
				// Following statement will update minCPU/minMemory value
				*currMin = *currMin - int64(math.Max(float64(deltaPerc), float64(deltaVal)))
			}
		}
	}

	return utils.NewGenericBucket(buckets), currBucket, nil
}

func getRecommendations(hvpa *autoscalingv1alpha1.Hvpa, currentReplicas int32, cpu, memory, currPodCPU, currPodMem int64, isScaleDown bool) (int64, int64, int64, error) {
	buckets, currentBucket, err := getBuckets(hvpa, currentReplicas)
	if err != nil {
		return 0, 0, 0, err
	}

	currentBucketHasCPU := currentBucket.HasValue(cpu, ResourceReplicas, ResourceCPU) == 0
	currentBucketHasMem := currentBucket.HasValue(memory, ResourceReplicas, ResourceMemory) == 0

	if currentBucketHasCPU && currentBucketHasMem {
		// no need to change bucket since recommended cpu AND memory fall in the same bucket
		// We can calculate new resource values based on current bucket itself then,
		// no need to use the generic bucket list anymore - overwrite:
		buckets = currentBucket
	} else if isScaleDown && (currentBucketHasCPU || currentBucketHasMem) {
		// scaling down - no need to change bucket since recommended cpu OR memory fall in the same bucket
		// This provides hysteresis while scaling down.
		// We can calculate new resource values based on current bucket itself then,
		// no need to use the generic bucket list anymore - overwrite:
		buckets = currentBucket
	}

	replicasByCPU, bucketCPU, _ := buckets.FindXValue(cpu, ResourceReplicas, ResourceCPU)
	replicasByMemory, bucketMemory, _ := buckets.FindXValue(memory, ResourceReplicas, ResourceMemory)

	// Recommend max - erring on the side of stability
	replicasReco := int64(math.Max(float64(replicasByCPU), float64(replicasByMemory)))
	var valueInterval utils.ValueInterval

	if replicasByCPU > replicasByMemory {
		replicasReco = replicasByCPU
		valueInterval = (*bucketCPU).GetIntervals()
	} else {
		replicasReco = replicasByMemory
		valueInterval = (*bucketMemory).GetIntervals()
	}

	if valueInterval.Intervals[ResourceCPU].MinValue == valueInterval.Intervals[ResourceCPU].MaxValue &&
		valueInterval.Intervals[ResourceMemory].MinValue == valueInterval.Intervals[ResourceMemory].MaxValue {
		// This interval supports only horizontal scaling with cpu and memory set to max values
		// Recalculate
		replicasByCPU := cpu / valueInterval.Intervals[ResourceCPU].MaxValue
		replicasByMemory := memory / valueInterval.Intervals[ResourceMemory].MaxValue
		replicasReco := int64(math.Max(float64(replicasByCPU), float64(replicasByMemory)))
		return replicasReco, valueInterval.Intervals[ResourceCPU].MaxValue, valueInterval.Intervals[ResourceMemory].MaxValue, nil
	}

	cpuReco := cpu / replicasReco
	memoryReco := memory / replicasReco

	// To prevent paradoxical situation around the boundary between the intervals where the desiredReplicas changes:
	if replicasReco > currentBucket.GetIntervals().Intervals[ResourceReplicas].MaxValue {
		// If scaling out, choose maximum of current requests and calculated requests
		cpuReco = int64(math.Max(float64(cpuReco), float64(currPodCPU)))
		memoryReco = int64(math.Max(float64(memoryReco), float64(currPodMem)))
	} else if replicasReco < currentBucket.GetIntervals().Intervals[ResourceReplicas].MinValue {
		// Don't scale if new per pod resource recommendations are higher than current
		if (currPodCPU != 0 && currPodCPU < cpuReco) || (currPodMem != 0 && currPodMem < memoryReco) {
			log.V(2).Info("Paradoxical scaling: No scaling recommended because recommendation for horizontal scale is \"scale in\", while for vertical scale is \"scale up\"", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
			replicasReco = int64(currentReplicas)
			cpuReco = currPodCPU
			memoryReco = currPodMem
			return replicasReco, cpuReco, memoryReco, nil
		}
	}

	if int32(replicasReco) != currentReplicas && hvpa.Spec.BaseResourcesPerReplica != nil {
		// Adjust for base usage for replicas
		var (
			deltaPercReco, deltaValReco, deltaPercReq, deltaValReq int64
			currReco, currReq                                      *int64
		)
		for resourceName, params := range hvpa.Spec.BaseResourcesPerReplica {
			if resourceName == corev1.ResourceCPU {
				currReq = &currPodCPU
				currReco = &cpuReco
			} else if resourceName == corev1.ResourceMemory {
				currReq = &currPodMem
				currReco = &memoryReco
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
				deltaValReq = valBase.MilliValue()
				valReco := resource.MustParse(*value)
				deltaValReco = valReco.MilliValue()
			}
			currBase := *currReq - int64(math.Max(float64(deltaPercReq), float64(deltaValReq)))
			if currBase < 0 {
				// This could happen if scaling is happening for the first time - current request might be set very small
				currBase = 0
			}
			// Following statement will update cpuReco/memoryReco value
			*currReco = *currReco - currBase + int64(math.Max(float64(deltaPercReco), float64(deltaValReco)))
		}
	}

	return replicasReco, cpuReco, memoryReco, nil
}

func getNewPodSpec(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, newpodcpu, newpodmemory, podCPUByVPA, podMemoryByVPA int64, podSpec *corev1.PodSpec, limitScalingParams *autoscalingv1alpha1.ScaleParams) (*corev1.PodSpec, *vpa_api.VerticalPodAutoscalerStatus) {
	len := len(vpaStatus.Recommendation.ContainerRecommendations)
	outVpaStatus := &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: make([]vpa_api.RecommendedContainerResources, 0, len),
		},
	}

	// Get fresh copy of podSpec. We need old container resources to calculate new limits
	newPodSpec := podSpec.DeepCopy()
	for _, reco := range vpaStatus.Recommendation.ContainerRecommendations {
		// Resources should be updated only for containers for which VPA recommendations are available
		for i := range newPodSpec.Containers {
			container := &newPodSpec.Containers[i]
			if container.Name == reco.ContainerName {
				vpaTargetCPU := reco.Target.Cpu().MilliValue()
				vpaTargetMemory := reco.Target.Memory().MilliValue()

				cpu := newpodcpu * vpaTargetCPU / podCPUByVPA
				memory := int64(float64(newpodmemory) * (float64(vpaTargetMemory) / float64(podMemoryByVPA))) // Prevents overflow

				newCPU := resource.NewScaledQuantity(cpu, resource.Milli)
				_ = newCPU.String() // cache string q.s
				newMem := resource.NewScaledQuantity(memory, resource.Milli)
				newMem.SetScaled(newMem.ScaledValue(resource.Kilo), resource.Kilo)

				newRequest := corev1.ResourceList{
					corev1.ResourceCPU:    *newCPU,
					corev1.ResourceMemory: *newMem,
				}
				newLimits := getScaledLimits(container.Resources.Limits, container.Resources.Requests, newRequest, *limitScalingParams)

				container.Resources.Requests = newRequest
				container.Resources.Limits = newLimits

				outVpaStatus.Recommendation.ContainerRecommendations = append(outVpaStatus.Recommendation.ContainerRecommendations,
					vpa_api.RecommendedContainerResources{
						Target:        newRequest,
						ContainerName: container.Name,
					})
				break
			}
		}
	}

	return newPodSpec, outVpaStatus
}

func getScalingRecommendations(
	hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *autoscalingv1alpha1.Hvpa,
	podSpec *corev1.PodSpec,
	currentReplicas int32,
) (
	scaledStatus *autoscalingv1alpha1.ScalingStatus,
	newPodSpec *corev1.PodSpec,
	resourceChanged bool,
	blockingReason autoscalingv1alpha1.BlockingReason,
	err error,
) {
	if vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.V(3).Info("VPA is not ready yet. Will not scale", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, nil, false, "", nil
	}

	if !vpaStatusConditionsSupported(vpaStatus, hvpa.Namespace+"/"+hvpa.Name) {
		return nil, nil, false, "", errors.New("VPA status conditions not supported")
	}

	var podMemoryByVPA, podCPUbyVPA, currentPodMemory, currentPodCPU resource.Quantity
	var totalMemoryByVPA, totalMemoryByHPA, totalCPUByVPA, totalCPUByHPA int64

	// Calculate total resource requests for containers in a pod for which VPA is enabled
	for i := range vpaStatus.Recommendation.ContainerRecommendations {
		recommendation := &vpaStatus.Recommendation.ContainerRecommendations[i]
		podMemoryByVPA.Add(*recommendation.Target.Memory())
		podCPUbyVPA.Add(*recommendation.Target.Cpu())

		for j := range podSpec.Containers {
			container := &podSpec.Containers[j]
			if container.Name == recommendation.ContainerName {
				if container.Resources.Requests != nil {
					currentPodMemory.Add(*container.Resources.Requests.Memory())
					currentPodCPU.Add(*container.Resources.Requests.Cpu())
				}
				break
			}
		}
	}

	currentPodMemoryVal := currentPodMemory.MilliValue()
	currentPodCPUVal := currentPodCPU.MilliValue()

	totalMemoryByVPA = podMemoryByVPA.MilliValue() * int64(currentReplicas)
	totalCPUByVPA = podCPUbyVPA.MilliValue() * int64(currentReplicas)
	if hpaStatus != nil {
		totalMemoryByHPA = currentPodMemoryVal * int64(hpaStatus.DesiredReplicas)
		totalCPUByHPA = currentPodCPUVal * int64(hpaStatus.DesiredReplicas)
	}

	totalMemoryRequired := int64(math.Max(float64(totalMemoryByHPA), float64(totalMemoryByVPA)))
	totalCPURequired := int64(math.Max(float64(totalCPUByHPA), float64(totalCPUByVPA)))

	actualMemDelta := totalMemoryRequired - (currentPodMemoryVal * int64(currentReplicas))
	actualCPUDelta := totalCPURequired - (currentPodCPUVal * int64(currentReplicas))
	if actualMemDelta == 0 && actualCPUDelta == 0 {
		// No scaling required
		return nil, nil, false, "", nil
	}

	// If either or both of memory and cpu are scaling up, consider it as a overall scale up - erring on the side of stability
	isScaleDown := actualCPUDelta <= 0 && actualMemDelta <= 0
	isScaleUp := !isScaleDown

	replicasReco, cpuReco, memoryReco, err := getRecommendations(hvpa, currentReplicas, totalCPURequired, totalMemoryRequired, currentPodCPUVal, currentPodMemoryVal, isScaleDown)
	if err != nil {
		return nil, nil, false, "", err
	}
	desiredReplicas := int32(replicasReco)
	if desiredReplicas == currentReplicas && cpuReco == currentPodCPUVal && memoryReco == currentPodMemoryVal {
		log.V(3).Info("Scaling Not required", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, nil, false, "", nil
	}

	desiredHpaStatus := &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
	}
	var desiredVpaStatus *vpa_api.VerticalPodAutoscalerStatus

	if cpuReco != currentPodCPUVal || memoryReco != currentPodMemoryVal {
		newPodSpec, desiredVpaStatus = getNewPodSpec(vpaStatus, cpuReco, memoryReco, podCPUbyVPA.MilliValue(), podMemoryByVPA.MilliValue(), podSpec, &hvpa.Spec.Vpa.LimitsRequestsGapScaleParams)
	}

	scaleUpMinDeltaMem := getThreshold(&hvpa.Spec.ScaleUp.MinChange.Memory, corev1.ResourceMemory, currentPodMemory)
	scaleDownMinDeltaMem := getThreshold(&hvpa.Spec.ScaleDown.MinChange.Memory, corev1.ResourceMemory, currentPodMemory)
	scaleUpMinDeltaCPU := getThreshold(&hvpa.Spec.ScaleUp.MinChange.CPU, corev1.ResourceCPU, currentPodCPU)
	scaleDownMinDeltaCPU := getThreshold(&hvpa.Spec.ScaleDown.MinChange.CPU, corev1.ResourceCPU, currentPodCPU)

	// The blockedScalingReason is populated with the reasons for blocking scaling with the following priority order:
	// UpdatePolicy > StabilizationWindow > MaintenanceWindow > MinChanged
	var blockedScalingReason autoscalingv1alpha1.BlockingReason

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

	// Check for minChange
	if ((actualMemDelta >= 0 && actualMemDelta < scaleUpMinDeltaMem) || (actualMemDelta <= 0 && -actualMemDelta < scaleDownMinDeltaMem)) &&
		((actualCPUDelta >= 0 && actualCPUDelta < scaleUpMinDeltaCPU) || (actualCPUDelta <= 0 && -actualCPUDelta < scaleDownMinDeltaCPU)) {
		// All deltas are less than minChange
		blockedScalingReason = autoscalingv1alpha1.BlockingReasonMinChange
	}

	// Check for maintenanceWindow mode
	if (isScaleDown && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeMaintenanceWindow) ||
		(isScaleUp && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeMaintenanceWindow) {
		maintenanceWindow := hvpa.Spec.MaintenanceTimeWindow
		if maintenanceWindow == nil {
			return nil, nil, false, blockedScalingReason, errors.New("scale up/down update mode is maintenanceWindow but maintenance time window is not provided")
		}
		maintenanceTimeWindow, err := utils.ParseMaintenanceTimeWindow(maintenanceWindow.Begin, maintenanceWindow.End)
		if err != nil {
			return nil, nil, false, blockedScalingReason, err
		}
		if !maintenanceTimeWindow.Contains(time.Now()) {
			// Overwrite
			blockedScalingReason = autoscalingv1alpha1.BlockingReasonMaintenanceWindow
		}
	}

	if (isScaleUp && scaleUpStabilizationWindow > lastScaleTimeDuration) ||
		(isScaleDown && scaleDownStabilizationWindow > lastScaleTimeDuration) {
		// Overwrite
		blockedScalingReason = autoscalingv1alpha1.BlockingReasonStabilizationWindow
	}

	if (isScaleUp && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff) ||
		(isScaleDown && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff) {
		// Overwrite
		blockedScalingReason = autoscalingv1alpha1.BlockingReasonUpdatePolicy
	}

	scaleStatus := getScalingStatusFrom(desiredHpaStatus, desiredVpaStatus, newPodSpec)

	return scaleStatus, newPodSpec, newPodSpec != nil && blockedScalingReason == "", blockedScalingReason, nil
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
