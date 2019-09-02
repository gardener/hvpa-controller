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
// Code generated by main. DO NOT EDIT.

package v1alpha1

import (
	v2beta2 "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	v1beta2 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BlockedScaling) DeepCopyInto(out *BlockedScaling) {
	*out = *in
	in.ScalingStatus.DeepCopyInto(&out.ScalingStatus)
	return
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
func (in *ChangeThreshold) DeepCopyInto(out *ChangeThreshold) {
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
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ChangeThreshold.
func (in *ChangeThreshold) DeepCopy() *ChangeThreshold {
	if in == nil {
		return nil
	}
	out := new(ChangeThreshold)
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
	if in.UpdatePolicy != nil {
		in, out := &in.UpdatePolicy, &out.UpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	in.Template.DeepCopyInto(&out.Template)
	return
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
	return
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
	return
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
		*out = make([]v2beta2.MetricSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
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
	return
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
	return
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
	if in.WeightBasedScalingIntervals != nil {
		in, out := &in.WeightBasedScalingIntervals, &out.WeightBasedScalingIntervals
		*out = make([]WeightBasedScalingInterval, len(*in))
		copy(*out, *in)
	}
	if in.TargetRef != nil {
		in, out := &in.TargetRef, &out.TargetRef
		*out = new(v2beta2.CrossVersionObjectReference)
		**out = **in
	}
	if in.ScaleUpStabilization != nil {
		in, out := &in.ScaleUpStabilization, &out.ScaleUpStabilization
		*out = new(ScaleStabilization)
		(*in).DeepCopyInto(*out)
	}
	if in.ScaleDownStabilization != nil {
		in, out := &in.ScaleDownStabilization, &out.ScaleDownStabilization
		*out = new(ScaleStabilization)
		(*in).DeepCopyInto(*out)
	}
	return
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
	if in.HpaUpdatePolicy != nil {
		in, out := &in.HpaUpdatePolicy, &out.HpaUpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.VpaUpdatePolicy != nil {
		in, out := &in.VpaUpdatePolicy, &out.VpaUpdatePolicy
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
	return
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
func (in *LastError) DeepCopyInto(out *LastError) {
	*out = *in
	in.LastUpdateTime.DeepCopyInto(&out.LastUpdateTime)
	return
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
func (in *ScaleStabilization) DeepCopyInto(out *ScaleStabilization) {
	*out = *in
	if in.Duration != nil {
		in, out := &in.Duration, &out.Duration
		*out = new(string)
		**out = **in
	}
	if in.MinCPUChange != nil {
		in, out := &in.MinCPUChange, &out.MinCPUChange
		*out = new(ChangeThreshold)
		(*in).DeepCopyInto(*out)
	}
	if in.MinMemChange != nil {
		in, out := &in.MinMemChange, &out.MinMemChange
		*out = new(ChangeThreshold)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleStabilization.
func (in *ScaleStabilization) DeepCopy() *ScaleStabilization {
	if in == nil {
		return nil
	}
	out := new(ScaleStabilization)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScalingStatus) DeepCopyInto(out *ScalingStatus) {
	*out = *in
	if in.LastScaleTime != nil {
		in, out := &in.LastScaleTime, &out.LastScaleTime
		*out = (*in).DeepCopy()
	}
	out.HpaStatus = in.HpaStatus
	in.VpaStatus.DeepCopyInto(&out.VpaStatus)
	return
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
	return
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
	if in.UpdatePolicy != nil {
		in, out := &in.UpdatePolicy, &out.UpdatePolicy
		*out = new(UpdatePolicy)
		(*in).DeepCopyInto(*out)
	}
	in.Template.DeepCopyInto(&out.Template)
	return
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
	return
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
	return
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

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WeightBasedScalingInterval) DeepCopyInto(out *WeightBasedScalingInterval) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WeightBasedScalingInterval.
func (in *WeightBasedScalingInterval) DeepCopy() *WeightBasedScalingInterval {
	if in == nil {
		return nil
	}
	out := new(WeightBasedScalingInterval)
	in.DeepCopyInto(out)
	return out
}
