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

	hvpav1alpha2 "github.com/gardener/hvpa-controller/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scaleFor = map[corev1.ResourceName]resource.Scale{
	corev1.ResourceCPU:    resource.Milli,
	corev1.ResourceMemory: resource.Mega,
}

func getScaleFor(name corev1.ResourceName) resource.Scale {
	return scaleFor[name]
}

// EffectiveScalingInterval explicitly defines a single effective scaling interval
// with a fixed single desired `replicas` value and the corresponding allowed range
// (maximum while scaling up and minimum while scaling down) for total as well as
// per-pod resources for the given desired `replicas`.
type EffectiveScalingInterval struct {
	// The desired replicas for this effective scaling interval
	Replicas int32
	// Applicable while scaling down
	MinResources corev1.ResourceList
	// Applicable while scaling up
	MaxResources corev1.ResourceList
}

func NameScaleInterval(si hvpav1alpha2.ScaleInterval, name corev1.ResourceName, format resource.Format) *resource.Quantity {
	var q *resource.Quantity
	switch name {
	case corev1.ResourceCPU:
		q = si.MaxCPU
	case corev1.ResourceMemory:
		q = si.MaxMemory
	}

	if q != nil {
		return q
	}

	return &resource.Quantity{Format: format}
}

func NameScaleParams(sp hvpav1alpha2.ScaleParams, name corev1.ResourceName) *hvpav1alpha2.ChangeParams {
	switch name {
	case corev1.ResourceCPU:
		return &sp.CPU
	case corev1.ResourceMemory:
		return &sp.Memory
	}

	return nil
}

// EffectiveScalingIntervals defines the contract to interact with effective scaling intervals.
type EffectiveScalingIntervals interface {
	IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue resource.Quantity) (bool, error)
	// If more than one replicas has the resource value in its range (due to overlap), then use biasReplicas
	// to resolve the conflict. If the biasReplicas is not among the candidate replicas then the lower of the replicas
	// should be returned.
	GetReplicasForResource(resourceName corev1.ResourceName, resourceValue resource.Quantity, biasReplicas int32) (int32, error)
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

func (e *effectiveScalingIntervals) IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue resource.Quantity) (bool, error) {
	if len(e.intervals) < int(replicas) {
		return false, ErrorOutOfRange
	}
	interval := e.intervals[replicas-1]
	return interval.IsResourceInRangeForReplica(replicas, resourceName, resourceValue)
}

func (e *effectiveScalingIntervals) GetReplicasForResource(resourceName corev1.ResourceName, resourceValue resource.Quantity, biasReplicas int32) (int32, error) {
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
	interval.MinResources = make(corev1.ResourceList)
	interval.MaxResources = make(corev1.ResourceList)
	return interval
}

// IsResourceInRangeForReplica returns true if resourceValue falls in interval e
func (e *EffectiveScalingInterval) IsResourceInRangeForReplica(replicas int32, resourceName corev1.ResourceName, resourceValue resource.Quantity) (bool, error) {
	if e.Replicas != replicas {
		return false, errors.New("replicas do not match the interval replicas")
	}

	if resourceValue.Cmp(*NewQuantity(resource.DecimalSI, func(q *resource.Quantity) {
		var s = getScaleFor(resourceName)
		q.SetScaled(e.MinResources.Name(resourceName, resource.DecimalSI).ScaledValue(s)*int64(replicas), s)
	})) < 0 {
		return false, nil // Less than min rsources
	}
	if resourceValue.Cmp(*NewQuantity(resource.DecimalSI, func(q *resource.Quantity) {
		var s = getScaleFor(resourceName)
		q.SetScaled(e.MaxResources.Name(resourceName, resource.DecimalSI).ScaledValue(s)*int64(replicas), s)
	})) > 0 {
		return false, nil // More than max resources
	}

	return true, nil
}

var (
	// ErrorOutOfRange is error when value is out of range
	ErrorOutOfRange error = errors.New("Value out of range")
)

// GetEffectiveScalingIntervals factory to construct effective scaling intervals from provided scaling intervals
func GetEffectiveScalingIntervals(hvpa *hvpav1alpha2.Hvpa, currentReplicas int32) (effectiveScalingIntervalsMap map[string]EffectiveScalingIntervals, err error) {
	var (
		containerMapIntervalMap        map[string][]*EffectiveScalingInterval = make(map[string][]*EffectiveScalingInterval)
		minReplicasGlobal              int32                                  = 1
		firstContainerName             string
		firstContainerFirstIntervalLen int
	)

	effectiveScalingIntervalsMap = make(map[string]EffectiveScalingIntervals)
	scalingOverlap := hvpa.Spec.ScalingIntervalsOverlap

	if hvpa.Spec.Hpa.Template.Spec.MinReplicas != nil {
		minReplicasGlobal = *hvpa.Spec.Hpa.Template.Spec.MinReplicas
	}

	if hvpa.Spec.Vpa.Template.Spec.ResourcePolicy == nil {
		return nil, fmt.Errorf("VPA resource policy not defined for hvpa: %q", client.ObjectKeyFromObject(hvpa))
	}

	// Since minAllowed is taken as the min resource value for the first interval for each container,
	// the effective scaling intervals generated using first scaleInterval can be different for each container.
	// But rest of the effective scaling intervals are going to be the same for all the containers because for rest of the scaleIntervals,
	// the min resource values are calculated using max resource of each interval and scalingOverlap which is common for all containers.
	for i := range hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies {
		container := &hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies[i]
		if container.Mode != nil && *container.Mode == vpa_api.ContainerScalingModeOff {
			continue
		}
		containerMapIntervalMap[container.ContainerName] = make([]*EffectiveScalingInterval, 0)
		if firstContainerName == "" {
			firstContainerName = container.ContainerName
		}
		minReplicas := minReplicasGlobal

		minResources := corev1.ResourceList{
			corev1.ResourceCPU:    container.MinAllowed.Cpu().DeepCopy(),
			corev1.ResourceMemory: container.MinAllowed.Memory().DeepCopy(),
		}
		for j, scaleInterval := range hvpa.Spec.ScaleIntervals {
			effectiveScalingIntervals, err := getLinearEffectiveScalingIntervals(container.MaxAllowed, scaleInterval, scalingOverlap, minResources, &minReplicas, client.ObjectKeyFromObject(hvpa).String())
			if err != nil {
				return nil, err
			}
			if container.ContainerName == firstContainerName && j == 0 {
				firstContainerFirstIntervalLen = len(effectiveScalingIntervals)
			}
			containerMapIntervalMap[container.ContainerName] = append(containerMapIntervalMap[container.ContainerName], effectiveScalingIntervals...)
			if container.ContainerName != firstContainerName {
				// For 2nd container onwards, since rest of the effective scaling intervals are same as for first container, append and break
				containerMapIntervalMap[container.ContainerName] = append(containerMapIntervalMap[container.ContainerName], containerMapIntervalMap[firstContainerName][firstContainerFirstIntervalLen:]...)
				break
			}
		}
	}

	for containerName, effectiveScalingIntervals := range containerMapIntervalMap {
		log.V(4).Info("hvpa", "containerName", containerName, "hvpa", client.ObjectKeyFromObject(hvpa))
		effectiveScalingIntervalsMap[containerName] = NewGenericEffectiveIntervals(effectiveScalingIntervals)
	}

	return effectiveScalingIntervalsMap, err
}

func getIntervalMaxResources(si hvpav1alpha2.ScaleInterval, maxAllowed corev1.ResourceList, hvpaKey string) (corev1.ResourceList, error) {
	var (
		errs []error
		rl   = corev1.ResourceList{}
	)

	iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
		var q = NameScaleInterval(si, name, format)
		if q.IsZero() {
			q = maxAllowed.Name(name, format)
		}

		if q.IsZero() {
			errs = append(errs, fmt.Errorf("atleast one of - VPA's maxAllowed %q or scale interval's %q - must be provided for hvpa: %s", name, name, hvpaKey))
		} else {
			rl[name] = q.DeepCopy()
		}
	})

	if len(errs) > 0 {
		return nil, utilerrors.NewAggregate(errs)
	}

	return rl, nil
}

func iterateCpuAndMemory(iterateFn func(name corev1.ResourceName, format resource.Format)) {
	for name, format := range map[corev1.ResourceName]resource.Format{
		corev1.ResourceCPU:    resource.DecimalSI,
		corev1.ResourceMemory: resource.BinarySI,
	} {
		iterateFn(name, format)
	}
}

func getLinearDeltaResources(minResources, maxResources corev1.ResourceList, minReplicas, maxReplicas int32, hvpaKey string) (corev1.ResourceList, int32, error) {
	if minReplicas >= maxReplicas {
		return nil, int32(0), fmt.Errorf("Invalid scale interval [%d, %d] for hvpa: %s", minReplicas, maxReplicas, hvpaKey)
	}

	var (
		numOfSubIntervals = maxReplicas - minReplicas
		delta             = corev1.ResourceList{}
	)

	iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
		delta[name] = *NewQuantity(format, func(q *resource.Quantity) {
			var s = getScaleFor(name)
			q.SetScaled((maxResources.Name(name, format).ScaledValue(s)-minResources.Name(name, format).ScaledValue(s))/int64(numOfSubIntervals), s)
		})
	})

	return delta, numOfSubIntervals, nil
}

func getLocalMaxResources(subInterval int32, min, delta corev1.ResourceList) corev1.ResourceList {
	var localMax = corev1.ResourceList{}
	iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
		localMax[name] = *NewQuantity(format, func(q *resource.Quantity) {
			var s = getScaleFor(name)
			q.SetScaled(min.Name(name, format).ScaledValue(s)+int64(subInterval)*delta.Name(name, format).ScaledValue(s), s)
		})
	})

	return localMax
}

// getLinearEffectiveScalingIntervals divides the supplied scale interval linearly into smaller effecting scaling intervals per integer replicas
// within the range of the supplied scale interval.  If the supplied scale interval spans only a single replica then
// only one effective scaling interval corresponding to it is returned,
func getLinearEffectiveScalingIntervals(vpaMaxAllowed corev1.ResourceList, scaleInterval hvpav1alpha2.ScaleInterval, scalingOverlap hvpav1alpha2.ResourceChangeParams, minResources corev1.ResourceList, minReplicas *int32, hvpa string) (effectiveScalingIntervals []*EffectiveScalingInterval, err error) {
	if minReplicas == nil {
		return nil, fmt.Errorf("minReplicas should be non-nil for hvpa: %s", hvpa)
	}

	replicas := minReplicas

	intervalMaxResources, err := getIntervalMaxResources(scaleInterval, vpaMaxAllowed, hvpa)
	if err != nil {
		return nil, err
	}

	if minResources == nil {
		return nil, fmt.Errorf("minResources should be non-nil for hvpa: %s", hvpa)
	}

	deltaResources, numOfSubIntervals, err := getLinearDeltaResources(minResources, intervalMaxResources, *replicas, scaleInterval.MaxReplicas, hvpa)
	if err != nil {
		return nil, err
	}

	for i := int32(0); i < numOfSubIntervals; i++ {
		var (
			localMaxResources = getLocalMaxResources(i+int32(1), minResources, deltaResources)

			esi = &EffectiveScalingInterval{
				Replicas:     *replicas,
				MinResources: corev1.ResourceList{},
				MaxResources: corev1.ResourceList{},
			}
		)

		iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
			esi.MinResources[name] = minResources.Name(name, format).DeepCopy()
			esi.MaxResources[name] = localMaxResources.Name(name, format).DeepCopy()
		})

		log.V(2).Info("hvpa intervals", "interval", esi, "hvpa", hvpa)
		effectiveScalingIntervals = append(effectiveScalingIntervals, esi)

		// Prepare for next iteration - total max of current interval is equal to total min of next interval
		prevReplicas := *replicas
		*replicas = *replicas + 1

		iterateCpuAndMemory(func(name corev1.ResourceName, format resource.Format) {
			minResources[name] = *NewQuantity(format, func(q *resource.Quantity) {
				var s = getScaleFor(name)
				q.SetScaled(localMaxResources.Name(name, format).ScaledValue(s)*int64(prevReplicas)/int64(*replicas), s)
			})
		})

		if scalingOverlap != nil {
			// Adjust min values for overlap
			for resourceName, params := range scalingOverlap {
				var (
					s            = getScaleFor(resourceName)
					minQ, minQOk = esi.MinResources[resourceName]
				)

				if !minQOk {
					log.V(3).Info("WARNING:", "unsupported resource in ScalingIntervalsOverlap", resourceName, "hvpa", hvpa)
					continue
				}

				esi.MinResources[resourceName] = func() resource.Quantity {
					var (
						chQ = getChangeQuantity(&params, minQ, s, MaxInt64)
						subQ = minQ.DeepCopy()
					)

					subQ.Sub(chQ)
					if subQ.Cmp(resource.Quantity{}) >= 0 {
						return subQ
					}

					return minQ
				}()
			}
		}
	}
	return effectiveScalingIntervals, nil
}

func QuantityPtr(q resource.Quantity) *resource.Quantity { return &q }

func getChangeQuantity(cp *hvpav1alpha2.ChangeParams, refQ resource.Quantity, s resource.Scale, chooseFn func(x, y int64) int64) resource.Quantity {
	if cp == nil {
		return refQ.DeepCopy()
	}

	var deltaPerc, deltaVal, delta int64

	if cp.Percentage != nil {
		deltaPerc = refQ.ScaledValue(s) * int64(*cp.Percentage) / int64(100)
	}

	if cp.Value != nil {
		deltaVal = QuantityPtr(resource.MustParse(*cp.Value)).ScaledValue(s)
	}

	if cp.Percentage != nil && cp.Value != nil {
		delta = chooseFn(deltaPerc, deltaVal)
	} else if cp.Percentage != nil {
		delta = deltaPerc
	} else if cp.Value != nil {
		delta = deltaVal
	} else {
		return refQ.DeepCopy()
	}

	return *NewQuantity(refQ.Format, func(q *resource.Quantity) {
		q.SetScaled(delta, s)
	})
}

func getThreshold(thresholdVals *hvpav1alpha2.ChangeParams, name corev1.ResourceName, currentVal *resource.Quantity) resource.Quantity {
	var (
		defaultThreshold = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("200M"),
		}
	)
	if thresholdVals == nil || (thresholdVals.Value == nil && thresholdVals.Percentage == nil) {
		return defaultThreshold.Name(name, currentVal.Format).DeepCopy()
	}

	return getChangeQuantity(thresholdVals, currentVal.DeepCopy(), getScaleFor(name), MinInt64)
}

func MinQuantity(x, y *resource.Quantity) *resource.Quantity {
	if x.Cmp(*y) > 0 {
		return y
	}
	return x
}

func MaxQuantity(x, y *resource.Quantity) *resource.Quantity {
	if x.Cmp(*y) < 0 {
		return y
	}
	return x
}

func MaxInt32(x, y int32) int32 {
	if x < y {
		return y
	}

	return x
}

func MaxInt64(x, y int64) int64 {
	if x < y {
		return y
	}

	return x
}

func MinInt64(x, y int64) int64 {
	if x > y {
		return y
	}

	return x
}

func NewQuantity(format resource.Format, mutateFn func(*resource.Quantity)) *resource.Quantity {
	var q = resource.Quantity{Format: format}

	mutateFn(&q)

	return &q
}
