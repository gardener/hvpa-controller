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

package utils

import (
	"errors"
	"math"
)

/*
TODO: Remove?
// AggregatedBuckets defines collection of buckets
type AggregatedBuckets interface {
	// Returns the number of buckets in the histogram.
	NumBuckets() int
	// Returns the index of the bucket to which the given value falls.
	// If the value is outside of the range covered by the histogram, it
	// returns the closest bucket (either the first or the last one).
	FindBucket(value float64) int
}

type aggregatedBuckets struct {
	aggregatedBuckets []Bucket
	xInterval         intervalMap
	yInterval         intervalMap
}
*/

// Bucket defines the size and nature of bucket.
// The argument "value" is product of values on "xAxis" and "yAxis".
// A Bucket can have any number of axis, with their minValue, maxValue and delta defined.
// To find "XValue" for a given "value", "delta" will be used to iterate over primary axis, the name for which should be passed as xAxis argument.
// "YValue" would be the value in yAxis, such that XValue * YValue = "value".
//
// A Bucket is said to contain the "value" if:
// 1. The "value" is greater than the product of minValues of xAxis and yAxis intervals, and
// 2. The "value" is less than the product of maxValues of xAxis and yAxis intervals
type Bucket interface {
	FindXValue(value int64, xAxis, yAxis AxisName) (int64, error)
	FindYValue(value int64, xAxis, yAxis AxisName) (int64, error)
	HasValue(value int64, xAxis, yAxis AxisName) int
	GetIntervals() valueInterval
	AddAxis(axisName AxisName, minValue, maxValue, delta int64) error
}

func newInterval(axis AxisName, minValue, maxValue, delta int64) (Interval, error) {
	if minValue <= 0 || maxValue < minValue || delta > maxValue-minValue {
		return Interval{}, errors.New("minValue must both be positive, and maxValue should be greater than minValue")
	}
	return Interval{
		MinValue: minValue,
		MaxValue: maxValue,
		delta:    delta,
	}, nil
}

// NewLinearBucket returns Bucket describing a linear bucket
func NewLinearBucket() (Bucket, error) {
	return &linearBucket{}, nil
}

// NewGenericBucket returns Bucket describing a collection of buckets
func NewGenericBucket(buckets []Bucket) Bucket {
	genericValueInterval := valueInterval{
		intervals: make(map[AxisName]Interval),
	}

	for _, bucket := range buckets {
		valueInterval := bucket.GetIntervals()

		for axisName, interval := range valueInterval.intervals {
			if _, ok := genericValueInterval.intervals[axisName]; !ok {
				genericValueInterval.intervals[axisName] = interval
				continue
			}
			currentInterval := genericValueInterval.intervals[axisName]
			if currentInterval.MinValue > interval.MinValue {
				currentInterval.MinValue = interval.MinValue
			}
			if currentInterval.MaxValue < interval.MaxValue {
				currentInterval.MaxValue = interval.MaxValue
			}
			genericValueInterval.intervals[axisName] = currentInterval
		}
	}
	return &genericBucket{buckets, genericValueInterval}
}

// Interval defines the range of interval
type Interval struct {
	MinValue int64
	MaxValue int64
	delta    int64 // used for iteration.
}

var (
	// ErrorOutOfRange is error when value is out of range
	ErrorOutOfRange error = errors.New("Value out of range")
)

// AxisName is the name of the axis for bucket intervals
type AxisName string

const (
	// ResourceCPU represents CPU in millicores (1core = 1000millicores).
	ResourceCPU AxisName = "cpu"
	// ResourceMemory represents memory, in bytes. (500Gi = 500GiB = 500 * 1024 * 1024 * 1024).
	ResourceMemory AxisName = "memory"
	// ResourceReplicas represents replicas.
	ResourceReplicas AxisName = "replicas"
)

type intervalMap map[AxisName]Interval

type valueInterval struct {
	intervals intervalMap
}

func (o *valueInterval) HasValue(value int64, xAxis, yAxis AxisName) int {
	minTotal := o.intervals[xAxis].MinValue * o.intervals[yAxis].MinValue
	maxTotal := o.intervals[xAxis].MaxValue * o.intervals[yAxis].MaxValue

	if value < minTotal {
		return -1
	}
	if value > maxTotal {
		return 1
	}
	return 0
}

type linearBucket struct {
	valueInterval
}

// Defines collection of buckets
type genericBucket struct {
	buckets []Bucket
	valueInterval
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

func (o *genericBucket) GetIntervals() valueInterval {
	if o == nil {
		return valueInterval{}
	}
	return o.valueInterval
}

func (o *genericBucket) FindXValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	bucketIdx, err := o.FindBucket(value, xAxis, yAxis)
	if err != nil {
		return 0, err
	}
	xValue, err := o.buckets[bucketIdx].FindXValue(value, xAxis, yAxis)
	return xValue, err
}

func (o *genericBucket) FindYValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	bucketIdx, err := o.FindBucket(value, xAxis, yAxis)
	if err != nil {
		return 0, err
	}
	yValue, err := o.buckets[bucketIdx].FindYValue(value, xAxis, yAxis)
	return yValue, err
}

func (o *genericBucket) AddAxis(axisName AxisName, minValue, maxValue, delta int64) error {
	return nil
}

func (o *linearBucket) AddAxis(axisName AxisName, minValue, maxValue, delta int64) error {
	if o.intervals == nil {
		o.intervals = make(map[AxisName]Interval)
	}
	interval, err := newInterval(axisName, minValue, maxValue, delta)
	o.intervals[axisName] = interval
	return err
}

func (o *linearBucket) GetIntervals() valueInterval {
	if o == nil {
		return valueInterval{}
	}
	return o.valueInterval
}

func (o *linearBucket) FindXValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	if o.HasValue(value, xAxis, yAxis) != 0 {
		// TODO: return whether value is below or above bucket
		return 0, ErrorOutOfRange
	}

	numberOfXIntervals := float64(o.intervals[xAxis].MaxValue-o.intervals[xAxis].MinValue+1) / float64(o.intervals[xAxis].delta)
	if numberOfXIntervals != math.Trunc(numberOfXIntervals) {
		return 0, errors.New("delta does not divide the interval wholly")
	}

	yDelta := (o.intervals[yAxis].MaxValue - o.intervals[yAxis].MinValue) / int64(numberOfXIntervals)

	x := o.intervals[xAxis].MinValue
	i, y := int64(1), int64(0)
	for ; x <= o.intervals[xAxis].MaxValue; x += o.intervals[xAxis].delta {
		y = o.intervals[yAxis].MinValue + i*yDelta
		if value < x*y {
			break
		}
		i++
	}
	return x, nil
}

func (o *linearBucket) FindYValue(value int64, xAxis, yAxis AxisName) (int64, error) {
	x, err := o.FindXValue(value, xAxis, yAxis)
	if err != nil {
		return 0, err
	}
	// TODO: round off according to y axis delta
	return int64(math.Round(float64(value) / float64(x))), nil
}
