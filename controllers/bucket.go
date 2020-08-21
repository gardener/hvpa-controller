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
	"math"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

const (
	// ResourceCPU represents CPU in millicores (1core = 1000millicores).
	ResourceCPU AxisName = "cpu"
	// ResourceMemory represents memory, in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024).
	ResourceMemory AxisName = "memory"
	// ResourceReplicas represents replicas.
	ResourceReplicas AxisName = "replicas"
)

// Bucket defines the size and nature of bucket.
// The argument "value" is product of values on "xAxis" and "yAxis".
// A Bucket can have any number of axis, with their minValue and maxValue defined.
// To find "XValue" for a given "value", we iterate over primary axis, the name for which should be passed as xAxis argument.
// "YValue" would be the value in yAxis, such that XValue * YValue = "value".
//
// A Bucket is said to contain the "value" if:
// 1. The "value" is greater than the product of minValues of xAxis and yAxis intervals, and
// 2. The "value" is less than the product of maxValues of xAxis and yAxis intervals
type Bucket interface {
	FindXValue(value int64, xAxis, yAxis AxisName) (int64, *Bucket, error)
	FindYValue(value int64, xAxis, yAxis AxisName) (int64, error)
	HasValue(value int64, xAxis, yAxis AxisName) int
	GetIntervals() ValueInterval
	AddAxis(axisName AxisName, minValue, maxValue int64) error
}

func newInterval(axis AxisName, minValue, maxValue int64) (Interval, error) {
	if minValue < 0 || maxValue < minValue {
		return Interval{}, errors.New("minValue must both be positive, and maxValue should be greater than minValue")
	}

	return Interval{
		MinValue: minValue,
		MaxValue: maxValue,
	}, nil
}

// NewLinearBucket returns Bucket describing a linear bucket
func NewLinearBucket() Bucket {
	return &linearBucket{}
}

// NewGenericBucket returns Bucket describing a collection of buckets
func NewGenericBucket(buckets []Bucket) Bucket {
	genericValueInterval := ValueInterval{
		Intervals: make(map[AxisName]Interval),
	}

	for _, bucket := range buckets {
		valueInterval := bucket.GetIntervals()
		log.V(4).Info("hvpa", "interval", valueInterval)

		for axisName, interval := range valueInterval.Intervals {
			if _, ok := genericValueInterval.Intervals[axisName]; !ok {
				genericValueInterval.Intervals[axisName] = interval
				continue
			}
			currentInterval := genericValueInterval.Intervals[axisName]
			if currentInterval.MinValue > interval.MinValue {
				currentInterval.MinValue = interval.MinValue
			}
			if currentInterval.MaxValue < interval.MaxValue {
				currentInterval.MaxValue = interval.MaxValue
			}
			genericValueInterval.Intervals[axisName] = currentInterval
		}
	}
	return &genericBucket{buckets, genericValueInterval}
}

// Interval defines the range of interval
type Interval struct {
	MinValue int64
	MaxValue int64
}

var (
	// ErrorOutOfRange is error when value is out of range
	ErrorOutOfRange error = errors.New("Value out of range")
)

// AxisName is the name of the axis for bucket intervals
type AxisName string

// IntervalMap defines axis with intervals
type IntervalMap map[AxisName]Interval

// ValueInterval has values for a bucket
type ValueInterval struct {
	Intervals IntervalMap
}

// HasValue checks if the interval containes a value
func (o *ValueInterval) HasValue(value int64, xAxis, yAxis AxisName) int {
	// TODO: check if axis with the provided names exist
	minTotal := o.Intervals[xAxis].MinValue * o.Intervals[yAxis].MinValue
	maxTotal := o.Intervals[xAxis].MaxValue * o.Intervals[yAxis].MaxValue

	if value < minTotal {
		return -1
	}
	if value > maxTotal {
		return 1
	}
	return 0
}

type linearBucket struct {
	ValueInterval
}

// Defines collection of buckets
type genericBucket struct {
	buckets []Bucket
	ValueInterval
}

func (o *genericBucket) NumBuckets() int {
	return len(o.buckets)
}

func (o *genericBucket) FindBucket(value int64, xAxis, yAxis AxisName) (int, error) {
	for i, bucket := range o.buckets {
		if bucket.HasValue(value, xAxis, yAxis) == 0 {
			return i, nil
		}
	}

	// TODO: return whether value is below or above bucket
	return 0, ErrorOutOfRange
}

func (o *genericBucket) GetIntervals() ValueInterval {
	if o == nil {
		return ValueInterval{}
	}
	return o.ValueInterval
}

func (o *genericBucket) FindXValue(value int64, xAxis, yAxis AxisName) (int64, *Bucket, error) {
	bucketIdx, err := o.FindBucket(value, xAxis, yAxis)
	if err != nil {
		return 0, nil, err
	}
	xValue, _, err := o.buckets[bucketIdx].FindXValue(value, xAxis, yAxis)
	return xValue, &o.buckets[bucketIdx], err
}

func (o *genericBucket) FindYValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	bucketIdx, err := o.FindBucket(value, xAxis, yAxis)
	if err != nil {
		return 0, err
	}
	yValue, err := o.buckets[bucketIdx].FindYValue(value, xAxis, yAxis)
	return yValue, err
}

func (o *genericBucket) AddAxis(axisName AxisName, minValue, maxValue int64) error {
	return nil
}

func (o *linearBucket) AddAxis(axisName AxisName, minValue, maxValue int64) error {
	if o.Intervals == nil {
		o.Intervals = make(map[AxisName]Interval)
	}
	interval, err := newInterval(axisName, minValue, maxValue)
	o.Intervals[axisName] = interval
	return err
}

func (o *linearBucket) GetIntervals() ValueInterval {
	if o == nil {
		return ValueInterval{}
	}
	return o.ValueInterval
}

func (o *linearBucket) FindXValue(value int64, xAxis, yAxis AxisName) (int64, *Bucket, error) {
	if o.HasValue(value, xAxis, yAxis) != 0 {
		// TODO: return whether value is below or above bucket
		return 0, nil, ErrorOutOfRange
	}

	numberOfXIntervals := o.Intervals[xAxis].MaxValue - o.Intervals[xAxis].MinValue + 1

	yDelta := (o.Intervals[yAxis].MaxValue - o.Intervals[yAxis].MinValue) / int64(numberOfXIntervals)

	x := o.Intervals[xAxis].MinValue
	i := int64(1)
	for ; x <= o.Intervals[xAxis].MaxValue; x++ {
		yMaxForX := o.Intervals[yAxis].MinValue + i*yDelta
		if value < x*yMaxForX {
			break
		}
		i++
	}
	b := Bucket(o)
	return x, &b, nil
}

func (o *linearBucket) FindYValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	x, _, err := o.FindXValue(value, xAxis, yAxis)
	if err != nil {
		return 0, err
	}
	if o.Intervals[yAxis].MinValue == o.Intervals[yAxis].MaxValue {
		// Special case: when only xValue should change, without any change in yValue
		return o.Intervals[yAxis].MinValue, nil
	}
	return int64(math.Round(float64(value) / float64(x))), nil
}

// GetBuckets factory to construct buckets from provided scaling intervals
func GetBuckets(hvpa *hvpav1alpha1.Hvpa, currentReplicas int32) (bucketList map[string]Bucket, currentBucket Bucket, err error) {
	var (
		containerMapBucketMap                map[string][]Bucket = make(map[string][]Bucket)
		containerBucketMap                   map[string]Bucket   = make(map[string]Bucket)
		currBucket                           Bucket
		minReplicasGlobal                    int32 = 1
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
		containerMapBucketMap[container.ContainerName] = make([]Bucket, 0)
		if i == 0 {
			firstContainerName = container.ContainerName
		}
		minReplicas := minReplicasGlobal

		if container.MinAllowed != nil {
			minCPU = container.MinAllowed.Cpu().MilliValue()
			minMemory = container.MinAllowed.Memory().MilliValue()
		}
		for j, scaleInterval := range hvpa.Spec.ScaleIntervals {
			bucket, curr, err := getLinearBuckets(scaleInterval, scalingOverlap, &minCPU, &minMemory, &minReplicas, currentReplicas, hvpa.Namespace+"/"+hvpa.Name)
			if err != nil {
				return nil, nil, err
			}
			if i == 0 && j == 0 {
				firstContainerFirstIntervalBucketLen = len(bucket)
			}
			containerMapBucketMap[container.ContainerName] = append(containerMapBucketMap[container.ContainerName], bucket...)
			if curr != nil {
				currBucket = curr
			}
			if i != 0 {
				// For 2nd container onwards, since rest of the buckets are same as for first container, append and break
				containerMapBucketMap[container.ContainerName] = append(containerMapBucketMap[container.ContainerName], containerMapBucketMap[firstContainerName][firstContainerFirstIntervalBucketLen:]...)
				break
			}
		}
	}

	for containerName, buckets := range containerMapBucketMap {
		log.V(4).Info("hvpa", "containerName", containerName, "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		containerBucketMap[containerName] = NewGenericBucket(buckets)
	}

	return containerBucketMap, currBucket, err
}

// Linear bucket factory
func getLinearBuckets(scaleInterval hvpav1alpha1.ScaleIntervals, scalingOverlap hvpav1alpha1.ResourceChangeParams, minCPU, minMemory *int64, minReplicas *int32, currentReplicas int32, hvpa string) (bucketList []Bucket, currentBucket Bucket, err error) {
	var (
		currMin    *int64
		buckets    []Bucket
		currBucket Bucket
	)
	replicas := minReplicas
	intervalMaxCPU := resource.MustParse(scaleInterval.MaxCPU)
	intervalMaxMemory := resource.MustParse(scaleInterval.MaxMemory)
	intervalMinCPU := *minCPU
	intervalMinMemory := *minMemory

	numOfSubIntervals := int64(scaleInterval.MaxReplicas - *replicas + 1)
	cpuDelta := (intervalMaxCPU.MilliValue() - intervalMinCPU) / numOfSubIntervals
	memDelta := (intervalMaxMemory.MilliValue() - intervalMinMemory) / numOfSubIntervals

	for i := int64(1); i <= numOfSubIntervals; i++ {
		localMaxCPU := intervalMinCPU + i*cpuDelta
		localMaxMem := intervalMinMemory + i*memDelta

		bucket := NewLinearBucket()
		err := bucket.AddAxis(ResourceReplicas, int64(*replicas), int64(*replicas))
		if err != nil {
			return nil, nil, err
		}
		err = bucket.AddAxis(ResourceCPU, int64(*minCPU), int64(localMaxCPU))
		if err != nil {
			return nil, nil, err
		}
		err = bucket.AddAxis(ResourceMemory, int64(*minMemory), int64(localMaxMem))
		if err != nil {
			return nil, nil, err
		}

		if currentReplicas == *replicas {
			currBucket = bucket
		}

		log.V(4).Info("hvpa intervals", "interval", bucket.GetIntervals(), "hvpa", hvpa)
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
	return buckets, currBucket, nil
}
