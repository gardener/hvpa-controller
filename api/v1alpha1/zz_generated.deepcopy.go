// +build !ignore_autogenerated

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

// autogenerated by controller-gen object, do not modify manually

package v1alpha1

import (
	"k8s.io/api/autoscaling/v2beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BlockedScaling) DeepCopyInto(out *BlockedScaling) {
	*out = *in
	in.ScalingStatus.DeepCopyInto(&out.ScalingStatus)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BlockedScaling.
func (in *BlockedScaling) DeepCopy() *BlockedScaling {
	if in == nil {
		return nil
	}
	out := new(BlockedScaling)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ChangeParams) DeepCopyInto(out *ChangeParams) {
	*out = *in
	if in.Value != nil {
		in, out := &in.Value, &out.Value
		*out = new(string)
		**out = **in
	}
	if in.Percentage != nil {
		in, out := &in.Percentage, &out.Percentage
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ChangeParams.
func (in *ChangeParams) DeepCopy() *ChangeParams {
	if in == nil {
		return nil
	}
	out := new(ChangeParams)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HpaSpec) DeepCopyInto(out *HpaSpec) {
	*out = *in
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.ScaleUp.DeepCopyInto(&out.ScaleUp)
	in.ScaleDown.DeepCopyInto(&out.ScaleDown)
	in.Template.DeepCopyInto(&out.Template)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HpaSpec.
func (in *HpaSpec) DeepCopy() *HpaSpec {
	if in == nil {
		return nil
	}
	out := new(HpaSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HpaStatus) DeepCopyInto(out *HpaStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HpaStatus.
func (in *HpaStatus) DeepCopy() *HpaStatus {
	if in == nil {
		return nil
	}
	out := new(HpaStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HpaTemplate) DeepCopyInto(out *HpaTemplate) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HpaTemplate.
func (in *HpaTemplate) DeepCopy() *HpaTemplate {
	if in == nil {
		return nil
	}
	out := new(HpaTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HpaTemplateSpec) DeepCopyInto(out *HpaTemplateSpec) {
	*out = *in
	if in.MinReplicas != nil {
		in, out := &in.MinReplicas, &out.MinReplicas
		*out = new(int32)
		**out = **in
	}
	if in.Metrics != nil {
		in, out := &in.Metrics, &out.Metrics
		*out = make([]v2beta1.MetricSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HpaTemplateSpec.
func (in *HpaTemplateSpec) DeepCopy() *HpaTemplateSpec {
	if in == nil {
		return nil
	}
	out := new(HpaTemplateSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Hvpa) DeepCopyInto(out *Hvpa) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Hvpa.
func (in *Hvpa) DeepCopy() *Hvpa {
	if in == nil {
		return nil
	}
	out := new(Hvpa)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Hvpa) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HvpaList) DeepCopyInto(out *HvpaList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Hvpa, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HvpaList.
func (in *HvpaList) DeepCopy() *HvpaList {
	if in == nil {
		return nil
	}
	out := new(HvpaList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *HvpaList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HvpaSpec) DeepCopyInto(out *HvpaSpec) {
	*out = *in
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	in.Hpa.DeepCopyInto(&out.Hpa)
	in.Vpa.DeepCopyInto(&out.Vpa)
	if in.ScaleIntervals != nil {
		in, out := &in.ScaleIntervals, &out.ScaleIntervals
		*out = make([]ScaleIntervals, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TargetRef != nil {
		in, out := &in.TargetRef, &out.TargetRef
		*out = new(v2beta1.CrossVersionObjectReference)
		**out = **in
	}
	if in.MaintenanceTimeWindow != nil {
		in, out := &in.MaintenanceTimeWindow, &out.MaintenanceTimeWindow
		*out = new(MaintenanceTimeWindow)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HvpaSpec.
func (in *HvpaSpec) DeepCopy() *HvpaSpec {
	if in == nil {
		return nil
	}
	out := new(HvpaSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HvpaStatus) DeepCopyInto(out *HvpaStatus) {
	*out = *in
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	if in.TargetSelector != nil {
		in, out := &in.TargetSelector, &out.TargetSelector
		*out = new(string)
		**out = **in
	}
	if in.HpaScaleUpUpdatePolicy != nil {
		in, out := &in.HpaScaleUpUpdatePolicy, &out.HpaScaleUpUpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.HpaScaleDownUpdatePolicy != nil {
		in, out := &in.HpaScaleDownUpdatePolicy, &out.HpaScaleDownUpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.VpaScaleUpUpdatePolicy != nil {
		in, out := &in.VpaScaleUpUpdatePolicy, &out.VpaScaleUpUpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.VpaScaleDownUpdatePolicy != nil {
		in, out := &in.VpaScaleDownUpdatePolicy, &out.VpaScaleDownUpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.LastBlockedScaling != nil {
		in, out := &in.LastBlockedScaling, &out.LastBlockedScaling
		*out = make([]*BlockedScaling, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(BlockedScaling)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	in.LastScaling.DeepCopyInto(&out.LastScaling)
	if in.LastError != nil {
		in, out := &in.LastError, &out.LastError
		*out = new(LastError)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HvpaStatus.
func (in *HvpaStatus) DeepCopy() *HvpaStatus {
	if in == nil {
		return nil
	}
	out := new(HvpaStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Interval) DeepCopyInto(out *Interval) {
	*out = *in
	if in.MinValue != nil {
		in, out := &in.MinValue, &out.MinValue
		*out = new(string)
		**out = **in
	}
	if in.MaxValue != nil {
		in, out := &in.MaxValue, &out.MaxValue
		*out = new(string)
		**out = **in
	}
	if in.Delta != nil {
		in, out := &in.Delta, &out.Delta
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Interval.
func (in *Interval) DeepCopy() *Interval {
	if in == nil {
		return nil
	}
	out := new(Interval)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LastError) DeepCopyInto(out *LastError) {
	*out = *in
	in.LastUpdateTime.DeepCopyInto(&out.LastUpdateTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LastError.
func (in *LastError) DeepCopy() *LastError {
	if in == nil {
		return nil
	}
	out := new(LastError)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MaintenanceTimeWindow) DeepCopyInto(out *MaintenanceTimeWindow) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MaintenanceTimeWindow.
func (in *MaintenanceTimeWindow) DeepCopy() *MaintenanceTimeWindow {
	if in == nil {
		return nil
	}
	out := new(MaintenanceTimeWindow)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScaleIntervals) DeepCopyInto(out *ScaleIntervals) {
	*out = *in
	in.CPU.DeepCopyInto(&out.CPU)
	in.Memory.DeepCopyInto(&out.Memory)
	in.Replicas.DeepCopyInto(&out.Replicas)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleIntervals.
func (in *ScaleIntervals) DeepCopy() *ScaleIntervals {
	if in == nil {
		return nil
	}
	out := new(ScaleIntervals)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScaleParams) DeepCopyInto(out *ScaleParams) {
	*out = *in
	in.CPU.DeepCopyInto(&out.CPU)
	in.Memory.DeepCopyInto(&out.Memory)
	in.Replicas.DeepCopyInto(&out.Replicas)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleParams.
func (in *ScaleParams) DeepCopy() *ScaleParams {
	if in == nil {
		return nil
	}
	out := new(ScaleParams)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScaleType) DeepCopyInto(out *ScaleType) {
	*out = *in
	in.UpdatePolicy.DeepCopyInto(&out.UpdatePolicy)
	in.MinChange.DeepCopyInto(&out.MinChange)
	if in.StabilizationDuration != nil {
		in, out := &in.StabilizationDuration, &out.StabilizationDuration
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleType.
func (in *ScaleType) DeepCopy() *ScaleType {
	if in == nil {
		return nil
	}
	out := new(ScaleType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScalingStatus) DeepCopyInto(out *ScalingStatus) {
	*out = *in
	if in.LastScaleTime != nil {
		in, out := &in.LastScaleTime, &out.LastScaleTime
		*out = new(v1.Time)
		(*in).DeepCopyInto(*out)
	}
	out.HpaStatus = in.HpaStatus
	in.VpaStatus.DeepCopyInto(&out.VpaStatus)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScalingStatus.
func (in *ScalingStatus) DeepCopy() *ScalingStatus {
	if in == nil {
		return nil
	}
	out := new(ScalingStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpdatePolicy) DeepCopyInto(out *UpdatePolicy) {
	*out = *in
	if in.UpdateMode != nil {
		in, out := &in.UpdateMode, &out.UpdateMode
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdatePolicy.
func (in *UpdatePolicy) DeepCopy() *UpdatePolicy {
	if in == nil {
		return nil
	}
	out := new(UpdatePolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VpaSpec) DeepCopyInto(out *VpaSpec) {
	*out = *in
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.ScaleUp.DeepCopyInto(&out.ScaleUp)
	in.ScaleDown.DeepCopyInto(&out.ScaleDown)
	in.Template.DeepCopyInto(&out.Template)
	in.LimitsRequestsGapScaleParams.DeepCopyInto(&out.LimitsRequestsGapScaleParams)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VpaSpec.
func (in *VpaSpec) DeepCopy() *VpaSpec {
	if in == nil {
		return nil
	}
	out := new(VpaSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VpaTemplate) DeepCopyInto(out *VpaTemplate) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VpaTemplate.
func (in *VpaTemplate) DeepCopy() *VpaTemplate {
	if in == nil {
		return nil
	}
	out := new(VpaTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VpaTemplateSpec) DeepCopyInto(out *VpaTemplateSpec) {
	*out = *in
	if in.ResourcePolicy != nil {
		in, out := &in.ResourcePolicy, &out.ResourcePolicy
		*out = new(v1beta2.PodResourcePolicy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VpaTemplateSpec.
func (in *VpaTemplateSpec) DeepCopy() *VpaTemplateSpec {
	if in == nil {
		return nil
	}
	out := new(VpaTemplateSpec)
	in.DeepCopyInto(out)
	return out
}
