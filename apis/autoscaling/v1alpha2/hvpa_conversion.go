package v1alpha2

import (
	"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts TO the Hub API version (v1alpha1) from this version (v1alpha2)
func (src *Hvpa) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha1.Hvpa)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert the Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Vpa = v1alpha1.VpaSpec{
		Selector:  src.Spec.Vpa.Selector,
		Deploy:    src.Spec.Vpa.Deploy,
		ScaleUp:   Convert_ScaleType_To_v1alpha1_ScaleType(src.Spec.Vpa.ScaleUp),
		ScaleDown: Convert_ScaleType_To_v1alpha1_ScaleType(src.Spec.Vpa.ScaleDown),
		Template: v1alpha1.VpaTemplate{
			ObjectMeta: src.Spec.Vpa.Template.ObjectMeta,
			Spec:       v1alpha1.VpaTemplateSpec(src.Spec.Vpa.Template.Spec),
		},
		LimitsRequestsGapScaleParams: Convert_ScaleParams_To_v1alpha1_ScaleParams(src.Spec.Vpa.LimitsRequestsGapScaleParams),
	}
	dst.Spec.WeightBasedScalingIntervals = Convert_WeightBasedScalingIntervals_To_v1alpha1_WeightBasedScalingIntervals(src.Spec.WeightBasedScalingIntervals)
	dst.Spec.TargetRef = Convert_v2_CrossVersionObjectReference_To_v2beta1_CrossVersionObjectReference(src.Spec.TargetRef)
	dst.Spec.MaintenanceTimeWindow = Convert_MaintenanceWindow_To_v1alpha1_MaintenanceWindow(src.Spec.MaintenanceTimeWindow)
	dst.Spec.Hpa.Selector = src.Spec.Hpa.Selector
	dst.Spec.Hpa.Deploy = src.Spec.Hpa.Deploy
	dst.Spec.Hpa.ScaleUp = Convert_ScaleType_To_v1alpha1_ScaleType(src.Spec.Hpa.ScaleUp)
	dst.Spec.Hpa.ScaleDown = Convert_ScaleType_To_v1alpha1_ScaleType(src.Spec.Hpa.ScaleDown)
	dst.Spec.Hpa.Template.ObjectMeta = src.Spec.Hpa.Template.ObjectMeta
	dst.Spec.Hpa.Template.Spec.MinReplicas = src.Spec.Hpa.Template.Spec.MinReplicas
	dst.Spec.Hpa.Template.Spec.MaxReplicas = src.Spec.Hpa.Template.Spec.MaxReplicas
	for _, metric := range src.Spec.Hpa.Template.Spec.Metrics {
		ms := autoscalingv2beta1.MetricSpec{
			Type:              autoscalingv2beta1.MetricSourceType(metric.Type),
			Object:            Convert_autoscalingv2_ObjectMetricSource_To_autoscalingv2beta1_ObjectMetricSource(metric.Object),
			Pods:              Convert_autoscalingv2_PodsMetricSource_To_autoscalingv2beta1_PodsMetricSource(metric.Pods),
			Resource:          Convert_autoscalingv2_ResourceMetricSource_To_autoscalingv2beta1_ResourceMetricSource(metric.Resource),
			ContainerResource: Convert_autoscalingv2_ContainerResourceMetricSource_To_autoscalingv2beta1_ContainerResourceMetricSource(metric.ContainerResource),
			External:          Convert_autoscalingv2_ExternalMetricSource_To_autoscalingv2beta1_ExternalMetricSource(metric.External),
		}
		dst.Spec.Hpa.Template.Spec.Metrics = append(dst.Spec.Hpa.Template.Spec.Metrics, ms)
	}

	// Convert the Status
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.TargetSelector = src.Status.TargetSelector
	dst.Status.HpaScaleUpUpdatePolicy = Convert_UpdatePolicy_To_v1alpha1_UpdatePolicy(src.Status.HpaScaleUpUpdatePolicy)
	dst.Status.HpaScaleDownUpdatePolicy = Convert_UpdatePolicy_To_v1alpha1_UpdatePolicy(src.Status.HpaScaleDownUpdatePolicy)
	dst.Status.VpaScaleUpUpdatePolicy = Convert_UpdatePolicy_To_v1alpha1_UpdatePolicy(src.Status.VpaScaleUpUpdatePolicy)
	dst.Status.VpaScaleDownUpdatePolicy = Convert_UpdatePolicy_To_v1alpha1_UpdatePolicy(src.Status.VpaScaleDownUpdatePolicy)
	dst.Status.VpaWeight = src.Status.VpaWeight
	dst.Status.HpaWeight = src.Status.HpaWeight
	dst.Status.OverrideScaleUpStabilization = src.Status.OverrideScaleUpStabilization
	for _, blockedScaling := range src.Status.LastBlockedScaling {
		dst.Status.LastBlockedScaling = append(dst.Status.LastBlockedScaling, &v1alpha1.BlockedScaling{
			Reason:        v1alpha1.BlockingReason(blockedScaling.Reason),
			ScalingStatus: Convert_ScalingStatus_To_v1alpha1_ScalingStatus(blockedScaling.ScalingStatus),
		})
	}
	dst.Status.LastScaling = Convert_ScalingStatus_To_v1alpha1_ScalingStatus(src.Status.LastScaling)
	dst.Status.LastError = Convert_LastError_to_v1alpha1_LastError(src.Status.LastError)

	return nil
}

// ConvertFrom converts FROM the Hub API version (v1alpha1) to this API version (v1alpha2)
func (dst *Hvpa) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha1.Hvpa)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert the Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Vpa = VpaSpec{
		Selector:  src.Spec.Vpa.Selector,
		Deploy:    src.Spec.Vpa.Deploy,
		ScaleUp:   Convert_v1alpha1_ScaleType_To_ScaleType(src.Spec.Vpa.ScaleUp),
		ScaleDown: Convert_v1alpha1_ScaleType_To_ScaleType(src.Spec.Vpa.ScaleDown),
		Template: VpaTemplate{
			ObjectMeta: src.Spec.Vpa.Template.ObjectMeta,
			Spec:       VpaTemplateSpec(src.Spec.Vpa.Template.Spec),
		},
		LimitsRequestsGapScaleParams: Convert_v1alpha1_ScaleParams_To_ScaleParams(src.Spec.Vpa.LimitsRequestsGapScaleParams),
	}
	dst.Spec.WeightBasedScalingIntervals = Convert_v1alpha1_WeightBasedScalingIntervals_To_WeightBasedScalingIntervals(src.Spec.WeightBasedScalingIntervals)
	dst.Spec.TargetRef = Convert_v2beta1_CrossVersionObjectReference_To_v2_CrossVersionObjectReference(src.Spec.TargetRef)
	dst.Spec.MaintenanceTimeWindow = Convert_v1alpha1_MaintenanceWindow_To_MaintenanceWindow(src.Spec.MaintenanceTimeWindow)
	dst.Spec.Hpa.Selector = src.Spec.Hpa.Selector
	dst.Spec.Hpa.Deploy = src.Spec.Hpa.Deploy
	dst.Spec.Hpa.ScaleUp = Convert_v1alpha1_ScaleType_To_ScaleType(src.Spec.Hpa.ScaleUp)
	dst.Spec.Hpa.ScaleDown = Convert_v1alpha1_ScaleType_To_ScaleType(src.Spec.Hpa.ScaleDown)
	dst.Spec.Hpa.Template.ObjectMeta = src.Spec.Hpa.Template.ObjectMeta
	dst.Spec.Hpa.Template.Spec.MinReplicas = src.Spec.Hpa.Template.Spec.MinReplicas
	dst.Spec.Hpa.Template.Spec.MaxReplicas = src.Spec.Hpa.Template.Spec.MaxReplicas
	for _, metric := range src.Spec.Hpa.Template.Spec.Metrics {
		ms := autoscalingv2.MetricSpec{
			Type:              autoscalingv2.MetricSourceType(metric.Type),
			Object:            Convert_autoscalingv2beta1_ObjectMetricSource_To_autoscalingv2_ObjectMetricSource(metric.Object),
			Pods:              Convert_autoscalingv2beta1_PodsMetricSource_To_autoscalingv2_PodsMetricSource(metric.Pods),
			Resource:          Convert_autoscalingv2beta1_ResourceMetricSource_To_autoscalingv2_ResourceMetricSource(metric.Resource),
			ContainerResource: Convert_autoscalingv2beta1_ContainerResourceMetricSource_To_autoscalingv2_ContainerResourceMetricSource(metric.ContainerResource),
			External:          Convert_autoscalingv2beta1_ExternalMetricSource_To_autoscalingv2_ExternalMetricSource(metric.External),
		}
		dst.Spec.Hpa.Template.Spec.Metrics = append(dst.Spec.Hpa.Template.Spec.Metrics, ms)
	}

	// Convert the Status
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.TargetSelector = src.Status.TargetSelector
	dst.Status.HpaScaleUpUpdatePolicy = Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(src.Status.HpaScaleUpUpdatePolicy)
	dst.Status.HpaScaleDownUpdatePolicy = Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(src.Status.HpaScaleDownUpdatePolicy)
	dst.Status.VpaScaleUpUpdatePolicy = Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(src.Status.VpaScaleUpUpdatePolicy)
	dst.Status.VpaScaleDownUpdatePolicy = Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(src.Status.VpaScaleDownUpdatePolicy)
	dst.Status.VpaWeight = src.Status.VpaWeight
	dst.Status.HpaWeight = src.Status.HpaWeight
	dst.Status.OverrideScaleUpStabilization = src.Status.OverrideScaleUpStabilization
	for _, blockedScaling := range src.Status.LastBlockedScaling {
		dst.Status.LastBlockedScaling = append(dst.Status.LastBlockedScaling, &BlockedScaling{
			Reason:        BlockingReason(blockedScaling.Reason),
			ScalingStatus: Convert_v1alpha1_ScalingStatus_To_ScalingStatus(blockedScaling.ScalingStatus),
		})
	}
	dst.Status.LastScaling = Convert_v1alpha1_ScalingStatus_To_ScalingStatus(src.Status.LastScaling)
	dst.Status.LastError = Convert_v1alpha1_LastError_to_LastError(src.Status.LastError)

	return nil
}

func Convert_v1alpha1_WeightBasedScalingIntervals_To_WeightBasedScalingIntervals(in []v1alpha1.WeightBasedScalingInterval) []WeightBasedScalingInterval {
	if in == nil {
		return nil
	}
	out := []WeightBasedScalingInterval{}
	for _, weightBasedScalingInterval := range in {
		out = append(out, WeightBasedScalingInterval(weightBasedScalingInterval))
	}
	return out
}

func Convert_WeightBasedScalingIntervals_To_v1alpha1_WeightBasedScalingIntervals(in []WeightBasedScalingInterval) []v1alpha1.WeightBasedScalingInterval {
	if in == nil {
		return nil
	}
	out := []v1alpha1.WeightBasedScalingInterval{}
	for _, weightBasedScalingInterval := range in {
		out = append(out, v1alpha1.WeightBasedScalingInterval(weightBasedScalingInterval))
	}
	return out
}

func Convert_v2beta1_CrossVersionObjectReference_To_v2_CrossVersionObjectReference(in *autoscalingv2beta1.CrossVersionObjectReference) *autoscalingv2.CrossVersionObjectReference {
	if in == nil {
		return nil
	}
	return &autoscalingv2.CrossVersionObjectReference{
		Kind:       in.Kind,
		Name:       in.Name,
		APIVersion: in.APIVersion,
	}
}

func Convert_v2_CrossVersionObjectReference_To_v2beta1_CrossVersionObjectReference(in *autoscalingv2.CrossVersionObjectReference) *autoscalingv2beta1.CrossVersionObjectReference {
	if in == nil {
		return nil
	}
	return &autoscalingv2beta1.CrossVersionObjectReference{
		Kind:       in.Kind,
		Name:       in.Name,
		APIVersion: in.APIVersion,
	}
}

func Convert_v1alpha1_MaintenanceWindow_To_MaintenanceWindow(in *v1alpha1.MaintenanceTimeWindow) *MaintenanceTimeWindow {
	if in == nil {
		return nil
	}
	mw := MaintenanceTimeWindow(*in)
	return &mw
}

func Convert_MaintenanceWindow_To_v1alpha1_MaintenanceWindow(in *MaintenanceTimeWindow) *v1alpha1.MaintenanceTimeWindow {
	if in == nil {
		return nil
	}
	mw := v1alpha1.MaintenanceTimeWindow(*in)
	return &mw
}

func Convert_v1alpha1_LastError_to_LastError(in *v1alpha1.LastError) *LastError {
	if in == nil {
		return nil
	}
	lastError := LastError(*in)
	return &lastError
}

func Convert_LastError_to_v1alpha1_LastError(in *LastError) *v1alpha1.LastError {
	if in == nil {
		return nil
	}
	lastError := v1alpha1.LastError(*in)
	return &lastError
}

func Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(in *v1alpha1.UpdatePolicy) *UpdatePolicy {
	if in == nil {
		return nil
	}
	up := UpdatePolicy(*in)
	return &up
}

func Convert_UpdatePolicy_To_v1alpha1_UpdatePolicy(in *UpdatePolicy) *v1alpha1.UpdatePolicy {
	if in == nil {
		return nil
	}
	up := v1alpha1.UpdatePolicy(*in)
	return &up
}

func Convert_v1alpha1_ScalingStatus_To_ScalingStatus(in v1alpha1.ScalingStatus) ScalingStatus {
	return ScalingStatus{
		LastScaleTime: in.LastScaleTime,
		HpaStatus: HpaStatus{
			CurrentReplicas: in.HpaStatus.CurrentReplicas,
			DesiredReplicas: in.HpaStatus.DesiredReplicas,
		},
		VpaStatus: in.VpaStatus,
	}
}

func Convert_ScalingStatus_To_v1alpha1_ScalingStatus(in ScalingStatus) v1alpha1.ScalingStatus {
	return v1alpha1.ScalingStatus{
		LastScaleTime: in.LastScaleTime,
		HpaStatus: v1alpha1.HpaStatus{
			CurrentReplicas: in.HpaStatus.CurrentReplicas,
			DesiredReplicas: in.HpaStatus.DesiredReplicas,
		},
		VpaStatus: in.VpaStatus,
	}
}

func Convert_v1alpha1_ScaleParams_To_ScaleParams(in v1alpha1.ScaleParams) ScaleParams {
	return ScaleParams{
		CPU:      ChangeParams(in.CPU),
		Memory:   ChangeParams(in.Memory),
		Replicas: ChangeParams(in.Replicas),
	}
}

func Convert_ScaleParams_To_v1alpha1_ScaleParams(in ScaleParams) v1alpha1.ScaleParams {
	return v1alpha1.ScaleParams{
		CPU:      v1alpha1.ChangeParams(in.CPU),
		Memory:   v1alpha1.ChangeParams(in.Memory),
		Replicas: v1alpha1.ChangeParams(in.Replicas),
	}
}

func Convert_v1alpha1_ScaleType_To_ScaleType(in v1alpha1.ScaleType) ScaleType {
	return ScaleType{
		UpdatePolicy:          UpdatePolicy(in.UpdatePolicy),
		MinChange:             Convert_v1alpha1_ScaleParams_To_ScaleParams(in.MinChange),
		StabilizationDuration: in.StabilizationDuration,
	}
}

func Convert_ScaleType_To_v1alpha1_ScaleType(in ScaleType) v1alpha1.ScaleType {
	return v1alpha1.ScaleType{
		UpdatePolicy:          v1alpha1.UpdatePolicy(in.UpdatePolicy),
		MinChange:             Convert_ScaleParams_To_v1alpha1_ScaleParams(in.MinChange),
		StabilizationDuration: in.StabilizationDuration,
	}
}

// Conversion methods taken from https://github.com/kubernetes/kubernetes/blob/v1.24.7/pkg/apis/autoscaling/v2beta1/conversion.go

func Convert_autoscalingv2beta1_ResourceMetricSource_To_autoscalingv2_ResourceMetricSource(in *autoscalingv2beta1.ResourceMetricSource) *autoscalingv2.ResourceMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2.ResourceMetricSource{}
	out.Name = in.Name
	utilization := in.TargetAverageUtilization
	averageValue := in.TargetAverageValue

	var metricType autoscalingv2.MetricTargetType
	if utilization == nil {
		metricType = autoscalingv2.AverageValueMetricType
	} else {
		metricType = autoscalingv2.UtilizationMetricType
	}
	out.Target = autoscalingv2.MetricTarget{
		Type:               metricType,
		AverageValue:       averageValue,
		AverageUtilization: utilization,
	}
	return &out
}

func Convert_autoscalingv2_ResourceMetricSource_To_autoscalingv2beta1_ResourceMetricSource(in *autoscalingv2.ResourceMetricSource) *autoscalingv2beta1.ResourceMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2beta1.ResourceMetricSource{}
	out.Name = in.Name
	out.TargetAverageUtilization = in.Target.AverageUtilization
	out.TargetAverageValue = in.Target.AverageValue
	return &out
}

func Convert_autoscalingv2beta1_ObjectMetricSource_To_autoscalingv2_ObjectMetricSource(in *autoscalingv2beta1.ObjectMetricSource) *autoscalingv2.ObjectMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2.ObjectMetricSource{}
	var metricType autoscalingv2.MetricTargetType
	if in.AverageValue == nil {
		metricType = autoscalingv2.ValueMetricType
	} else {
		metricType = autoscalingv2.AverageValueMetricType
	}
	out.Target = autoscalingv2.MetricTarget{
		Type:         metricType,
		Value:        &in.TargetValue,
		AverageValue: in.AverageValue,
	}
	out.DescribedObject = autoscalingv2.CrossVersionObjectReference{
		Kind:       in.Target.Kind,
		Name:       in.Target.Name,
		APIVersion: in.Target.APIVersion,
	}
	out.Metric = autoscalingv2.MetricIdentifier{
		Name:     in.MetricName,
		Selector: in.Selector,
	}
	return &out
}

func Convert_autoscalingv2_ObjectMetricSource_To_autoscalingv2beta1_ObjectMetricSource(in *autoscalingv2.ObjectMetricSource) *autoscalingv2beta1.ObjectMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2beta1.ObjectMetricSource{}
	if in.Target.Value != nil {
		out.TargetValue = *in.Target.Value
	}
	out.AverageValue = in.Target.AverageValue

	out.Target = autoscalingv2beta1.CrossVersionObjectReference{
		Kind:       in.DescribedObject.Kind,
		Name:       in.DescribedObject.Name,
		APIVersion: in.DescribedObject.APIVersion,
	}
	out.MetricName = in.Metric.Name
	out.Selector = in.Metric.Selector

	return &out
}

func Convert_autoscalingv2beta1_PodsMetricSource_To_autoscalingv2_PodsMetricSource(in *autoscalingv2beta1.PodsMetricSource) *autoscalingv2.PodsMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2.PodsMetricSource{}
	targetAverageValue := &in.TargetAverageValue
	metricType := autoscalingv2.AverageValueMetricType

	out.Target = autoscalingv2.MetricTarget{
		Type:         metricType,
		AverageValue: targetAverageValue,
	}
	out.Metric = autoscalingv2.MetricIdentifier{
		Name:     in.MetricName,
		Selector: in.Selector,
	}
	return &out
}

func Convert_autoscalingv2_PodsMetricSource_To_autoscalingv2beta1_PodsMetricSource(in *autoscalingv2.PodsMetricSource) *autoscalingv2beta1.PodsMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2beta1.PodsMetricSource{}
	if in.Target.AverageValue != nil {
		targetAverageValue := *in.Target.AverageValue
		out.TargetAverageValue = targetAverageValue
	}

	out.MetricName = in.Metric.Name
	out.Selector = in.Metric.Selector

	return &out
}

func Convert_autoscalingv2beta1_ContainerResourceMetricSource_To_autoscalingv2_ContainerResourceMetricSource(in *autoscalingv2beta1.ContainerResourceMetricSource) *autoscalingv2.ContainerResourceMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2.ContainerResourceMetricSource{}
	out.Name = in.Name
	utilization := in.TargetAverageUtilization
	averageValue := in.TargetAverageValue

	var metricType autoscalingv2.MetricTargetType
	if utilization == nil {
		metricType = autoscalingv2.AverageValueMetricType
	} else {
		metricType = autoscalingv2.UtilizationMetricType
	}
	out.Target = autoscalingv2.MetricTarget{
		Type:               metricType,
		AverageValue:       averageValue,
		AverageUtilization: utilization,
	}
	return &out
}

func Convert_autoscalingv2_ContainerResourceMetricSource_To_autoscalingv2beta1_ContainerResourceMetricSource(in *autoscalingv2.ContainerResourceMetricSource) *autoscalingv2beta1.ContainerResourceMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2beta1.ContainerResourceMetricSource{}
	out.Name = v1.ResourceName(in.Name)
	out.TargetAverageUtilization = in.Target.AverageUtilization
	out.TargetAverageValue = in.Target.AverageValue
	return &out
}

func Convert_autoscalingv2beta1_ExternalMetricSource_To_autoscalingv2_ExternalMetricSource(in *autoscalingv2beta1.ExternalMetricSource) *autoscalingv2.ExternalMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2.ExternalMetricSource{}
	value := in.TargetValue
	averageValue := in.TargetAverageValue

	var metricType autoscalingv2.MetricTargetType
	if value == nil {
		metricType = autoscalingv2.AverageValueMetricType
	} else {
		metricType = autoscalingv2.ValueMetricType
	}

	out.Target = autoscalingv2.MetricTarget{
		Type:         metricType,
		Value:        value,
		AverageValue: averageValue,
	}

	out.Metric = autoscalingv2.MetricIdentifier{
		Name:     in.MetricName,
		Selector: in.MetricSelector,
	}
	return &out
}

func Convert_autoscalingv2_ExternalMetricSource_To_autoscalingv2beta1_ExternalMetricSource(in *autoscalingv2.ExternalMetricSource) *autoscalingv2beta1.ExternalMetricSource {
	if in == nil {
		return nil
	}
	out := autoscalingv2beta1.ExternalMetricSource{}
	out.MetricName = in.Metric.Name
	out.TargetValue = in.Target.Value
	out.TargetAverageValue = in.Target.AverageValue
	out.MetricSelector = in.Metric.Selector
	return &out
}
