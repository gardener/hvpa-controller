package v1alpha2

import (
	"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

func (src *Hvpa) ConvertTo(dstRaw conversion.Hub) error {
	return nil
}

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
	dst.Spec.WeightBasedScalingIntervals = Convert_v1alpha1_WeightBasedScalingIntervals_To_v1alpha2_WeightBasedScalingIntervals(src.Spec.WeightBasedScalingIntervals)
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
			Object:            Convert_v2beta1_ObjectMetricSource_To_autoscaling_ObjectMetricSource(metric.Object),
			Pods:              Convert_v2beta1_PodsMetricSource_To_autoscaling_PodsMetricSource(metric.Pods),
			Resource:          Convert_v2beta1_ResourceMetricSource_To_autoscaling_ResourceMetricSource(metric.Resource),
			ContainerResource: Convert_v2beta1_ContainerResourceMetricSource_To_autoscaling_ContainerResourceMetricSource(metric.ContainerResource),
			External:          Convert_v2beta1_ExternalMetricSource_To_autoscaling_ExternalMetricSource(metric.External),
		}
		dst.Spec.Hpa.Template.Spec.Metrics = append(dst.Spec.Hpa.Template.Spec.Metrics, ms)
	}

	// Convert the Status
	dst.Status.Replicas = src.Spec.Replicas
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

func Convert_v1alpha1_WeightBasedScalingIntervals_To_v1alpha2_WeightBasedScalingIntervals(in []v1alpha1.WeightBasedScalingInterval) []WeightBasedScalingInterval {
	if in == nil {
		return nil
	}
	out := []WeightBasedScalingInterval{}
	for _, weightBasedScalingInterval := range in {
		out = append(out, WeightBasedScalingInterval(weightBasedScalingInterval))
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

func Convert_v1alpha1_MaintenanceWindow_To_MaintenanceWindow(in *v1alpha1.MaintenanceTimeWindow) *MaintenanceTimeWindow {
	if in == nil {
		return nil
	}
	mw := MaintenanceTimeWindow(*in)
	return &mw

}

func Convert_v1alpha1_LastError_to_LastError(in *v1alpha1.LastError) *LastError {
	if in == nil {
		return nil
	}
	lastError := LastError(*in)
	return &lastError
}

func Convert_v1alpha1_UpdatePolicy_To_UpdatePolicy(in *v1alpha1.UpdatePolicy) *UpdatePolicy {
	if in == nil {
		return nil
	}
	up := UpdatePolicy(*in)
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

func Convert_v1alpha1_ScaleParams_To_ScaleParams(in v1alpha1.ScaleParams) ScaleParams {
	return ScaleParams{
		CPU:      ChangeParams(in.CPU),
		Memory:   ChangeParams(in.Memory),
		Replicas: ChangeParams(in.Replicas),
	}
}

func Convert_v1alpha1_ScaleType_To_ScaleType(in v1alpha1.ScaleType) ScaleType {
	return ScaleType{
		UpdatePolicy:          UpdatePolicy(in.UpdatePolicy),
		MinChange:             Convert_v1alpha1_ScaleParams_To_ScaleParams(in.MinChange),
		StabilizationDuration: in.StabilizationDuration,
	}
}

// Conversion methods taken from https://github.com/kubernetes/kubernetes/blob/v1.24.7/pkg/apis/autoscaling/v2beta1/conversion.go

func Convert_v2beta1_ResourceMetricSource_To_autoscaling_ResourceMetricSource(in *autoscalingv2beta1.ResourceMetricSource) *autoscalingv2.ResourceMetricSource {
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

func Convert_v2beta1_ObjectMetricSource_To_autoscaling_ObjectMetricSource(in *autoscalingv2beta1.ObjectMetricSource) *autoscalingv2.ObjectMetricSource {
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

func Convert_v2beta1_PodsMetricSource_To_autoscaling_PodsMetricSource(in *autoscalingv2beta1.PodsMetricSource) *autoscalingv2.PodsMetricSource {
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

func Convert_v2beta1_ContainerResourceMetricSource_To_autoscaling_ContainerResourceMetricSource(in *autoscalingv2beta1.ContainerResourceMetricSource) *autoscalingv2.ContainerResourceMetricSource {
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

func Convert_v2beta1_ExternalMetricSource_To_autoscaling_ExternalMetricSource(in *autoscalingv2beta1.ExternalMetricSource) *autoscalingv2.ExternalMetricSource {
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
