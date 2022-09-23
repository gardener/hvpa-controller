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

// Package validation is used to validate all the HVPA CRD objects
package validation

import (
	"time"

	"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	"github.com/gardener/hvpa-controller/utils"
	"k8s.io/apimachinery/pkg/api/resource"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateHvpa validates a HVPA and returns a list of errors.
func ValidateHvpa(hvpa *v1alpha1.Hvpa) field.ErrorList {
	return internalValidateHvpa(hvpa)
}

func internalValidateHvpa(hvpa *v1alpha1.Hvpa) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateHvpaSpec(&hvpa.Spec, field.NewPath("spec"))...)
	return allErrs
}

func validateHvpaSpec(spec *v1alpha1.HvpaSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if spec == nil {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}

	if nil == spec.TargetRef {
		allErrs = append(allErrs, field.Required(fldPath.Child("targetRef"), "TargetRef is required"))
	}

	allErrs = append(allErrs, validateMaintenanceWindow(spec.MaintenanceTimeWindow, field.NewPath("spec.maintenanceTimeWindow"))...)
	allErrs = append(allErrs, validateHpaSpec(&spec.Hpa, field.NewPath("spec.hpa"))...)
	allErrs = append(allErrs, validateVpaSpec(&spec.Vpa, field.NewPath("spec.vpa"))...)

	// TODO: More validations

	return allErrs
}

func validateMaintenanceWindow(maintenance *v1alpha1.MaintenanceTimeWindow, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if maintenance == nil {
		return allErrs
	}

	_, err := utils.ParseMaintenanceTimeWindow(maintenance.Begin, maintenance.End)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("begin/end"), maintenance, err.Error()))
	}

	return allErrs
}

func validateHpaSpec(hpaSpec *v1alpha1.HpaSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if hpaSpec.Selector == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("selector"), ""))
	} else {
		allErrs = append(allErrs, v1validation.ValidateLabelSelector(hpaSpec.Selector, fldPath.Child("selector"))...)
		if len(hpaSpec.Selector.MatchLabels)+len(hpaSpec.Selector.MatchExpressions) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), hpaSpec.Selector, "empty selector is invalid for HPA"))
		}
	}

	allErrs = append(allErrs, validateScaleType(&hpaSpec.ScaleUp, fldPath.Child("scaleUp"))...)
	allErrs = append(allErrs, validateScaleType(&hpaSpec.ScaleDown, fldPath.Child("scaleDown"))...)

	selector, err := metav1.LabelSelectorAsSelector(hpaSpec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), hpaSpec.Selector, "invalid label selector"))
	} else {
		allErrs = append(allErrs, validateHpaSpecTemplate(&hpaSpec.Template, selector, fldPath.Child("template"))...)
	}

	return allErrs
}

func validateScaleParams(scaleParams *v1alpha1.ScaleParams, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if scaleParams == nil {
		return allErrs
	}

	if scaleParams.CPU.Value != nil {
		if _, err := resource.ParseQuantity(*scaleParams.CPU.Value); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("cpu", "value"), *scaleParams.CPU.Value, "Invalid min CPU change"))
		}
	}
	if scaleParams.CPU.Percentage != nil {
		if *scaleParams.CPU.Percentage < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("cpu", "percentage"), *scaleParams.CPU.Percentage, "Invalid min CPU change"))
		}
	}
	if scaleParams.Memory.Value != nil {
		if _, err := resource.ParseQuantity(*scaleParams.Memory.Value); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("memory", "value"), *scaleParams.Memory.Value, "Invalid min memory change"))
		}
	}
	if scaleParams.Memory.Percentage != nil {
		if *scaleParams.Memory.Percentage < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("memory", "percentage"), *scaleParams.Memory.Percentage, "Invalid min memory change"))
		}
	}

	return allErrs
}

func validateScaleType(scaleType *v1alpha1.ScaleType, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if scaleType == nil {
		return allErrs
	}

	if scaleType.UpdatePolicy.UpdateMode != nil &&
		*scaleType.UpdatePolicy.UpdateMode != v1alpha1.UpdateModeAuto &&
		*scaleType.UpdatePolicy.UpdateMode != v1alpha1.UpdateModeOff &&
		*scaleType.UpdatePolicy.UpdateMode != v1alpha1.UpdateModeMaintenanceWindow {
		validVals := []string{
			v1alpha1.UpdateModeAuto,
			v1alpha1.UpdateModeOff,
			v1alpha1.UpdateModeMaintenanceWindow,
		}
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("updatePolicy", "updateMode"), *scaleType.UpdatePolicy.UpdateMode, validVals))
	}

	if scaleType.StabilizationDuration != nil {
		if _, err := time.ParseDuration(*scaleType.StabilizationDuration); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("stabilizationDuration"), *scaleType.StabilizationDuration, "invalid stabilization duration"))
		}
	}

	allErrs = append(allErrs, validateScaleParams(&scaleType.MinChange, fldPath.Child("minChange"))...)

	return allErrs
}

func validateVpaSpec(vpaSpec *v1alpha1.VpaSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if vpaSpec.Selector == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("selector"), ""))
	} else {
		allErrs = append(allErrs, v1validation.ValidateLabelSelector(vpaSpec.Selector, fldPath.Child("selector"))...)
		if len(vpaSpec.Selector.MatchLabels)+len(vpaSpec.Selector.MatchExpressions) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), vpaSpec.Selector, "empty selector is invalid for HPA"))
		}
	}

	selector, err := metav1.LabelSelectorAsSelector(vpaSpec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), vpaSpec.Selector, "invalid label selector"))
	} else {
		allErrs = append(allErrs, validateVpaSpecTemplate(&vpaSpec.Template, selector, fldPath.Child("template"))...)
	}

	allErrs = append(allErrs, validateScaleType(&vpaSpec.ScaleUp, fldPath.Child("scaleUp"))...)
	allErrs = append(allErrs, validateScaleType(&vpaSpec.ScaleDown, fldPath.Child("scaleDown"))...)
	allErrs = append(allErrs, validateScaleParams(&vpaSpec.LimitsRequestsGapScaleParams, fldPath.Child("limitsRequestsGapScaleParams"))...)

	return allErrs
}

func validateHpaSpecTemplate(template *v1alpha1.HpaTemplate, selector labels.Selector, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if template == nil {
		allErrs = append(allErrs, field.Required(fldPath, ""))
	} else {
		if !selector.Empty() {
			// Verify that the selector matches the labels in template.
			labels := labels.Set(template.Labels)
			if !selector.Matches(labels) {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("metadata", "labels"), template.Labels, "`selector` does not match template `labels`"))
			}
		}
		allErrs = append(allErrs, validateHpaTemplateSpec(&template.Spec, fldPath.Child("spec"))...)

		allErrs = append(allErrs, v1validation.ValidateLabels(template.Labels, fldPath.Child("labels"))...)
		allErrs = append(allErrs, apimachineryvalidation.ValidateAnnotations(template.Annotations, fldPath.Child("annotations"))...)
	}

	return allErrs
}

func validateHpaTemplateSpec(spec *v1alpha1.HpaTemplateSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec == nil {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}

	if *spec.MinReplicas < 1 {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("minReplicas"), spec.MinReplicas, []string{"greater than 0"}))
	}
	if spec.MaxReplicas < *spec.MinReplicas {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxReplicas"), spec.MaxReplicas, "maxReplicas in hpa should be more than minReplicas"))
	}
	return allErrs
}

func validateVpaSpecTemplate(template *v1alpha1.VpaTemplate, selector labels.Selector, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if template == nil {
		allErrs = append(allErrs, field.Required(fldPath, ""))
	} else {
		if !selector.Empty() {
			// Verify that the selector matches the labels in template.
			labels := labels.Set(template.Labels)
			if !selector.Matches(labels) {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("metadata", "labels"), template.Labels, "`selector` does not match template `labels`"))
			}
		}

		allErrs = append(allErrs, v1validation.ValidateLabels(template.Labels, fldPath.Child("labels"))...)
		allErrs = append(allErrs, apimachineryvalidation.ValidateAnnotations(template.Annotations, fldPath.Child("annotations"))...)
	}

	return allErrs
}
