// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"errors"
	"fmt"
	"math"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

// EffectiveScalingInterval explicitly defines a single effective scaling interval
// with a fixed single desired `replicas` value and the corresponding allowed range
// (maximum while scaling up and minimum while scaling down) for total as well as
// per-pod resources for the given desired `replicas`.
type EffectiveScalingInterval struct {
	// The desired replicas for this effective scaling interval
	Replicas int32
	// Applicable while scaling down
	MinResources ResourceList
	// Applicable while scaling up
	MaxResources ResourceList
}

// ResourceList is a set of (resource name, quantity) pairs.
type ResourceList map[corev1.ResourceName]int64

// EffectiveScalingIntervals defines the contract to interact with effective scaling intervals.
type EffectiveScalingIntervals interface {
	IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue int64) (bool, error)
	// If more than one replicas has the resource value in its range (due to overlap), then use biasReplicas
	// to resolve the conflict. If the biasReplicas is not among the candidate replicas then the lower of the replicas
	// should be returned.
	GetReplicasForResource(resourceName corev1.ResourceName, resourceValue int64, biasReplicas int32) (int32, error)
}

// effectiveScalingIntervals contains the EffectiveScalingInterval indexed by their corresponding replicas count.
type effectiveScalingIntervals struct {
	intervals []*EffectiveScalingInterval
}

// NewGenericEffectiveIntervals returns EffectiveScalingIntervals interface
func NewGenericEffectiveIntervals(scalingIntervals []*EffectiveScalingInterval) EffectiveScalingIntervals {
	return &effectiveScalingIntervals{
		intervals: scalingIntervals,
	}
}

func (e *effectiveScalingIntervals) IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue int64) (bool, error) {
	if len(e.intervals) < int(replicas) {
		return false, ErrorOutOfRange
	}
	interval := e.intervals[replicas-1]
	return interval.IsResourceInRangeForReplica(replicas, resourceName, resourceValue)
}

func (e *effectiveScalingIntervals) GetReplicasForResource(resourceName corev1.ResourceName, resourceValue int64, biasReplicas int32) (int32, error) {
	// first check in the biasReplica interval
	inRange, err := e.IsResourceInRangeForReplica(biasReplicas, resourceName, resourceValue)
	if inRange {
		return biasReplicas, nil
	}

	for _, interval := range e.intervals {
		inRange, err = interval.IsResourceInRangeForReplica(interval.Replicas, resourceName, resourceValue)
		if err != nil {
			return interval.Replicas, err
		}
		if inRange {
			return interval.Replicas, nil
		}
	}
	return 0, ErrorOutOfRange
}

// NewEffectiveScalingInterval returns effective scaling interval
func NewEffectiveScalingInterval() *EffectiveScalingInterval {
	interval := &EffectiveScalingInterval{}
	interval.MinResources = make(ResourceList)
	interval.MaxResources = make(ResourceList)
	return interval
}

// IsResourceInRangeForReplica returns true if resourceValue falls in interval e
func (e *EffectiveScalingInterval) IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue int64) (bool, error) {
	if e.Replicas != replicas {
		return false, errors.New("replicas do not match the interval replicas")
	}
	minValue := int64(replicas) * e.MinResources[resourceName]
	maxValue := int64(replicas) * e.MaxResources[resourceName]

	if resourceValue >= minValue && resourceValue <= maxValue {
		return true, nil
	}
	return false, nil
}

var (
	// ErrorOutOfRange is error when value is out of range
	ErrorOutOfRange error = errors.New("Value out of range")
)

// GetBuckets factory to construct buckets from provided scaling intervals
func GetBuckets(hvpa *hvpav1alpha1.Hvpa, currentReplicas int32) (bucketList map[string]EffectiveScalingIntervals, err error) {
	var (
		containerMapBucketMap                map[string][]*EffectiveScalingInterval = make(map[string][]*EffectiveScalingInterval)
		containerBucketMap                   map[string]EffectiveScalingIntervals   = make(map[string]EffectiveScalingIntervals)
		minReplicasGlobal                    int32                                  = 1
		firstContainerName                   string
		firstContainerFirstIntervalBucketLen int
	)
	scalingOverlap := hvpa.Spec.ScalingIntervalsOverlap

	if hvpa.Spec.Hpa.Template.Spec.MinReplicas != nil {
		minReplicasGlobal = *hvpa.Spec.Hpa.Template.Spec.MinReplicas
	}

	// Since minAllowed is taken as the min resource value for the first interval for each container,
	// the buckets generated using first scaleInterval can be different for each container.
	// But rest of the buckets are going to be the same for all the containers because for rest of the scaleIntervals,
	// the min resource values are calculated using max resource of each interval and scalingOverlap which is common for all containers.
	for i := range hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies {
		container := &hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies[i]
		if container.Mode != nil && *container.Mode == vpa_api.ContainerScalingModeOff {
			continue
		}
		minCPU, minMemory := int64(0), int64(0)
		containerMapBucketMap[container.ContainerName] = make([]*EffectiveScalingInterval, 0)
		if firstContainerName == "" {
			firstContainerName = container.ContainerName
		}
		minReplicas := minReplicasGlobal

		if container.MinAllowed != nil {
			minCPU = container.MinAllowed.Cpu().MilliValue()
			minMemory = container.MinAllowed.Memory().MilliValue()
		}
		for j, scaleInterval := range hvpa.Spec.ScaleIntervals {
			bucket, err := getLinearBuckets(container.MaxAllowed, scaleInterval, scalingOverlap, &minCPU, &minMemory, &minReplicas, currentReplicas, hvpa.Namespace+"/"+hvpa.Name)
			if err != nil {
				return nil, err
			}
			if container.ContainerName == firstContainerName && j == 0 {
				firstContainerFirstIntervalBucketLen = len(bucket)
			}
			containerMapBucketMap[container.ContainerName] = append(containerMapBucketMap[container.ContainerName], bucket...)
			if container.ContainerName != firstContainerName {
				// For 2nd container onwards, since rest of the buckets are same as for first container, append and break
				containerMapBucketMap[container.ContainerName] = append(containerMapBucketMap[container.ContainerName], containerMapBucketMap[firstContainerName][firstContainerFirstIntervalBucketLen:]...)
				break
			}
		}
	}

	for containerName, buckets := range containerMapBucketMap {
		log.V(4).Info("hvpa", "containerName", containerName, "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		containerBucketMap[containerName] = NewGenericEffectiveIntervals(buckets)
	}

	return containerBucketMap, err
}

// Linear bucket factory
func getLinearBuckets(VPAMaxAllowed corev1.ResourceList, scaleInterval hvpav1alpha1.ScaleIntervals, scalingOverlap hvpav1alpha1.ResourceChangeParams, minCPU, minMemory *int64, minReplicas *int32, currentReplicas int32, hvpa string) (bucketList []*EffectiveScalingInterval, err error) {
	var (
		currMin *int64
		buckets []*EffectiveScalingInterval
	)
	replicas := minReplicas
	intervalMaxCPU := scaleInterval.MaxCPU
	if intervalMaxCPU == nil {
		if VPAMaxAllowed == nil || VPAMaxAllowed.Cpu().IsZero() {
			msg := fmt.Sprintf("atleast one of - VPA's maxAllowed CPU or scale interval's maxCPU - must be provided for hvpa: %s", hvpa)
			return nil, errors.New(msg)
		}
		intervalMaxCPU = VPAMaxAllowed.Cpu()
	}
	intervalMaxMemory := scaleInterval.MaxMemory
	if intervalMaxMemory == nil {
		if VPAMaxAllowed == nil || VPAMaxAllowed.Memory().IsZero() {
			msg := fmt.Sprintf("atleast one of - VPA's maxAllowed memory or scale interval's maxMemory - must be provided for hvpa: %s", hvpa)
			return nil, errors.New(msg)
		}
		intervalMaxMemory = VPAMaxAllowed.Memory()
	}
	intervalMinCPU := *minCPU
	intervalMinMemory := *minMemory

	numOfSubIntervals := int64(scaleInterval.MaxReplicas - *replicas + 1)
	cpuDelta := (intervalMaxCPU.MilliValue() - intervalMinCPU) / numOfSubIntervals
	memDelta := (intervalMaxMemory.MilliValue() - intervalMinMemory) / numOfSubIntervals

	for i := int64(1); i <= numOfSubIntervals; i++ {
		localMaxCPU := intervalMinCPU + i*cpuDelta
		localMaxMem := intervalMinMemory + i*memDelta

		bucket := NewEffectiveScalingInterval()
		bucket.Replicas = *replicas
		bucket.MinResources[corev1.ResourceCPU] = *minCPU
		bucket.MaxResources[corev1.ResourceCPU] = localMaxCPU

		bucket.MinResources[corev1.ResourceMemory] = *minMemory
		bucket.MaxResources[corev1.ResourceMemory] = localMaxMem

		log.V(2).Info("hvpa intervals", "interval", bucket, "hvpa", hvpa)
		buckets = append(buckets, bucket)

		// Prepare for next iteration - total max of current bucket is equal to total min of next bucket
		prevReplicas := *replicas
		*replicas = *replicas + 1
		*minCPU = localMaxCPU * int64(prevReplicas) / int64(*replicas)
		*minMemory = localMaxMem * int64(prevReplicas) / int64(*replicas)

		if scalingOverlap != nil {
			// Adjust min values for overlap
			for resourceName, params := range scalingOverlap {
				deltaPerc := int64(0)
				deltaVal := int64(0)
				if resourceName == corev1.ResourceCPU {
					currMin = minCPU
				} else if resourceName == corev1.ResourceMemory {
					currMin = minMemory
				} else {
					log.V(3).Info("WARNING:", "unsupported resource in ScalingIntervalsOverlap", resourceName, "hvpa", hvpa)
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
	return buckets, nil
}
