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
	err := linearBucket.AddAxis(ResourceReplicas, int64(minReplicas), int64(maxReplicas), 1)
	if err != nil {
		return nil, err
	}

	maxCPU := resource.MustParse(scaleInterval.MaxCPU)
	err = linearBucket.AddAxis(ResourceCPU, minCPU, maxCPU.MilliValue(), 1)
	if err != nil {
		return nil, err
	}

	maxMemory := resource.MustParse(scaleInterval.MaxMemory)
	err = linearBucket.AddAxis(ResourceMemory, minMemory, maxMemory.MilliValue(), 1)
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

func getBuckets(hvpa *autoscalingv1alpha1.Hvpa) (utils.Bucket, error) {
	var buckets []utils.Bucket

	minReplicas := *hvpa.Spec.Hpa.Template.Spec.MinReplicas
	minCPU, minMemory := getMinAllowed(hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies)

	for _, scaleInterval := range hvpa.Spec.ScaleIntervals {
		bucket, err := getLinearBucket(&scaleInterval, minCPU, minMemory, minReplicas)
		if err != nil {
			return nil, err
		}

		buckets = append(buckets, bucket)

		// Prepare for next iteration
		maxCPU := resource.MustParse(scaleInterval.MaxCPU)
		maxMemory := resource.MustParse(scaleInterval.MaxMemory)

		minReplicas = scaleInterval.MaxReplicas + 1
		minCPU = maxCPU.MilliValue() * int64(scaleInterval.MaxReplicas) / int64(minReplicas)
		minMemory = maxMemory.MilliValue() * int64(scaleInterval.MaxReplicas) / int64(minReplicas)
	}

	return utils.NewGenericBucket(buckets), nil
}

func getRecommendations(hvpa *autoscalingv1alpha1.Hvpa, currentReplicas int32, cpu, memory int64) (int64, int64, int64, error) {
	buckets, err := getBuckets(hvpa)
	if err != nil {
		return 0, 0, 0, err
	}

	replicasByCPU, bucketCPU, _ := buckets.FindXValue(cpu, ResourceReplicas, ResourceCPU)
	replicasByMemory, bucketMemory, _ := buckets.FindXValue(memory, ResourceReplicas, ResourceMemory)

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

	return replicasReco, cpuReco, memoryReco, nil
}

func getNewPodSpec(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, newcpu, newmemory, podCPUByVPA, podMemoryByVPA int64, podSpec *corev1.PodSpec, limitScalingParams *autoscalingv1alpha1.ScaleParams) (*corev1.PodSpec, *vpa_api.VerticalPodAutoscalerStatus) {
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

				cpu := newcpu * vpaTargetCPU / podCPUByVPA
				memory := int64(float64(newmemory) * (float64(vpaTargetMemory) / float64(podMemoryByVPA)))
				// Use maximum of recommended by VPA and calculated since resource usage tends to depend on base usage, eg. caching in case of memory.
				maxCPU := int64(math.Max(float64(cpu), float64(vpaTargetCPU)))
				maxMemory := int64(math.Max(float64(memory), float64(vpaTargetMemory)))

				newCPU := resource.NewScaledQuantity(maxCPU, resource.Milli)
				_ = newCPU.String() // cache string q.s
				newMem := resource.NewScaledQuantity(maxMemory, resource.Milli)
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

	totalMemoryByVPA = podMemoryByVPA.MilliValue() * int64(currentReplicas)
	totalCPUByVPA = podCPUbyVPA.MilliValue() * int64(currentReplicas)
	if hpaStatus != nil {
		totalMemoryByHPA = currentPodMemory.MilliValue() * int64(hpaStatus.DesiredReplicas)
		totalCPUByHPA = currentPodCPU.MilliValue() * int64(hpaStatus.DesiredReplicas)
	}

	totalMemoryRequired := int64(math.Max(float64(totalMemoryByHPA), float64(totalMemoryByVPA)))
	totalCPURequired := int64(math.Max(float64(totalCPUByHPA), float64(totalCPUByVPA)))

	actualMemDelta := totalMemoryRequired - (currentPodMemory.MilliValue() * int64(currentReplicas))
	actualCPUDelta := totalCPURequired - (currentPodCPU.MilliValue() * int64(currentReplicas))
	if actualMemDelta == 0 && actualCPUDelta == 0 {
		// No scaling required
		return nil, nil, false, "", nil
	}

	replicasReco, cpuReco, memoryReco, err := getRecommendations(hvpa, currentReplicas, totalCPURequired, totalMemoryRequired)
	if err != nil {
		return nil, nil, false, "", err
	}
	desiredReplicas := int32(replicasReco)

	desiredHpaStatus := &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
	}

	newPodSpec, desiredVpaStatus := getNewPodSpec(vpaStatus, cpuReco, memoryReco, podCPUbyVPA.MilliValue(), podMemoryByVPA.MilliValue(), podSpec, &hvpa.Spec.Vpa.LimitsRequestsGapScaleParams)

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
	if (actualMemDelta <= 0 && actualCPUDelta <= 0 && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeMaintenanceWindow) ||
		(actualMemDelta >= 0 && actualCPUDelta >= 0 && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeMaintenanceWindow) {
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

	if ((actualMemDelta >= 0 || actualCPUDelta >= 0) && scaleUpStabilizationWindow > lastScaleTimeDuration) ||
		((actualMemDelta <= 0 || actualCPUDelta <= 0) && scaleDownStabilizationWindow > lastScaleTimeDuration) {
		// Overwrite
		blockedScalingReason = autoscalingv1alpha1.BlockingReasonStabilizationWindow
	}

	if ((actualMemDelta >= 0 || actualCPUDelta >= 0) && hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff) ||
		((actualMemDelta <= 0 || actualCPUDelta <= 0) && hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode != nil && *hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff) {
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
