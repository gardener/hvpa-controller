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

package controllers

import (
	"context"
	"fmt"
	"reflect"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	kubernetesinternalautoscaling "k8s.io/kubernetes/pkg/apis/autoscaling"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
)

// TODO: use client library instead when it starts to support update retries
//
//	see https://github.com/kubernetes/kubernetes/issues/21479
type updateHpaFunc func(hpa *autoscaling.HorizontalPodAutoscaler)
type updateHpaV2Func func(hpa *autoscalingv2.HorizontalPodAutoscaler)

func (r *HvpaReconciler) Convert_v2beta1_HorizontalPodAutoscalerList_To_v2(in *autoscaling.HorizontalPodAutoscalerList) (*autoscalingv2.HorizontalPodAutoscalerList, error) {
	out := &autoscalingv2.HorizontalPodAutoscalerList{}
	internalHpas := &kubernetesinternalautoscaling.HorizontalPodAutoscalerList{}
	err := r.Scheme.Convert(in, internalHpas, context.TODO())
	if err != nil {
		return nil, err
	}
	err = r.Scheme.Convert(internalHpas, out, context.TODO())
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *HvpaReconciler) Convert_v2_HorizontalPodAutoscalerList_To_v2beta1(in *autoscalingv2.HorizontalPodAutoscalerList) (*autoscaling.HorizontalPodAutoscalerList, error) {
	out := &autoscaling.HorizontalPodAutoscalerList{}
	internalHpas := &kubernetesinternalautoscaling.HorizontalPodAutoscalerList{}
	err := r.Scheme.Convert(in, internalHpas, context.TODO())
	if err != nil {
		return nil, err
	}
	err = r.Scheme.Convert(internalHpas, out, context.TODO())
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *HvpaReconciler) Convert_v2beta1_HPA_to_v2(in *autoscaling.HorizontalPodAutoscaler) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	out := &autoscalingv2.HorizontalPodAutoscaler{}
	internal := &kubernetesinternalautoscaling.HorizontalPodAutoscaler{}
	err := r.Scheme.Convert(in, internal, context.TODO())
	if err != nil {
		return nil, err
	}
	err = r.Scheme.Convert(internal, out, context.TODO())
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *HvpaReconciler) Convert_v2_HPA_to_v2beta1(in *autoscalingv2.HorizontalPodAutoscaler) (*autoscaling.HorizontalPodAutoscaler, error) {
	out := &autoscaling.HorizontalPodAutoscaler{}
	internal := &kubernetesinternalautoscaling.HorizontalPodAutoscaler{}
	err := r.Scheme.Convert(in, internal, context.TODO())
	if err != nil {
		return nil, err
	}
	err = r.Scheme.Convert(internal, out, context.TODO())
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *HvpaReconciler) claimHpas(hvpa *autoscalingv1alpha1.Hvpa, selector labels.Selector, hpas *autoscaling.HorizontalPodAutoscalerList) ([]*autoscaling.HorizontalPodAutoscaler, error) {
	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing Machines (see #42639).
	canAdoptFunc := RecheckDeletionTimestamp(func() (metav1.Object, error) {
		foundHvpa := &autoscalingv1alpha1.Hvpa{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: hvpa.Name, Namespace: hvpa.Namespace}, foundHvpa)
		if err != nil {
			return nil, err
		}
		if foundHvpa.UID != hvpa.UID {
			return nil, fmt.Errorf("original %v/%v hvpa gone: got uid %v, wanted %v", hvpa.Namespace, hvpa.Name, foundHvpa.UID, hvpa.UID)
		}
		return foundHvpa, nil
	})
	cm := NewHvpaControllerRefManager(r, hvpa, selector, controllerKindHvpa, canAdoptFunc)
	return cm.ClaimHpas(hpas)
}

// The returned bool value can be used to tell if the hpa is actually updated.
func hpaSpecNeedChange(hvpa *autoscalingv1alpha1.Hvpa, hpa *autoscaling.HorizontalPodAutoscaler) bool {
	return *hvpa.Spec.Hpa.Template.Spec.MinReplicas != *hpa.Spec.MinReplicas ||
		hvpa.Spec.Hpa.Template.Spec.MaxReplicas != hpa.Spec.MaxReplicas ||
		!reflect.DeepEqual(hvpa.Spec.Hpa.Template.Spec.Metrics, hpa.Spec.Metrics) ||
		!reflect.DeepEqual(hvpa.Spec.TargetRef, &hpa.Spec.ScaleTargetRef)
}

// UpdateHpaWithRetries updates a hpa with given applyUpdate function. Note that hpa not found error is ignored.
func (r *HvpaReconciler) UpdateHpaWithRetries(namespace, name string, applyUpdate updateHpaFunc) (*autoscaling.HorizontalPodAutoscaler, error) {
	hpa := &autoscaling.HorizontalPodAutoscaler{}

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		if err = r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, hpa); err != nil {
			return err
		}
		hpa = hpa.DeepCopy()
		// Apply the update, then attempt to push it to the apiserver.
		applyUpdate(hpa)
		return r.Update(context.TODO(), hpa)
	})

	// Ignore the precondition violated error, this machine is already updated
	// with the desired label.
	if retryErr == errorsutil.ErrPreconditionViolated {
		log.V(4).Info("Hpa %s precondition doesn't hold, skip updating it.", namespace+"/"+name)
		retryErr = nil
	}

	return hpa, retryErr
}
func (r *HvpaReconciler) UpdateHpaV2WithRetries(namespace, name string, applyUpdate updateHpaV2Func) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		if err = r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, hpa); err != nil {
			return err
		}
		hpa = hpa.DeepCopy()
		// Apply the update, then attempt to push it to the apiserver.
		applyUpdate(hpa)
		return r.Update(context.TODO(), hpa)
	})

	// Ignore the precondition violated error, this machine is already updated
	// with the desired label.
	if retryErr == errorsutil.ErrPreconditionViolated {
		log.V(4).Info("Hpa %s precondition doesn't hold, skip updating it.", namespace+"/"+name)
		retryErr = nil
	}

	return hpa, retryErr
}

func (r *HvpaReconciler) syncHpaSpec(hpaList []*autoscaling.HorizontalPodAutoscaler, hvpa *autoscalingv1alpha1.Hvpa) error {
	for _, hpa := range hpaList {
		if hpaChanged := hpaSpecNeedChange(hvpa, hpa); hpaChanged {
			if !r.IsAutoscalingV2Enabled {
				_, err := r.UpdateHpaWithRetries(hpa.Namespace, hpa.Name,
					func(hpaToUpdate *autoscaling.HorizontalPodAutoscaler) {
						hpaToUpdate.Spec.MinReplicas = hvpa.Spec.Hpa.Template.Spec.MinReplicas
						hpaToUpdate.Spec.MaxReplicas = hvpa.Spec.Hpa.Template.Spec.MaxReplicas
						hpaToUpdate.Spec.Metrics = hvpa.Spec.Hpa.Template.Spec.Metrics
						hpaToUpdate.Spec.ScaleTargetRef = *hvpa.Spec.TargetRef
					})
				if err != nil {
					return fmt.Errorf("error in updating hpaTemplateSpec to hpa %q: %v", hpa.Name, err)
				}
				log.V(2).Info("HPA spec sync", "HPA", hpa.Name, "HVPA", hvpa.Namespace+"/"+hvpa.Name)
			} else {
				v2Hpa, err := r.Convert_v2beta1_HPA_to_v2(hpa)
				if err != nil {
					return err
				}
				v2HpaMetricSpecList := []autoscalingv2.MetricSpec{}
				for _, metricSpec := range hvpa.Spec.Hpa.Template.Spec.Metrics {
					v2HpaMetricSpec := &autoscalingv2.MetricSpec{}
					internalHpaMetricSpec := &kubernetesinternalautoscaling.MetricSpec{}
					err = r.Scheme.Convert(metricSpec, internalHpaMetricSpec, context.TODO())
					if err != nil {
						return err
					}
					err = r.Scheme.Convert(internalHpaMetricSpec, v2HpaMetricSpec, context.TODO())
					if err != nil {
						return err
					}
					v2HpaMetricSpecList = append(v2HpaMetricSpecList, *v2HpaMetricSpec)
				}
				v2CrossVersionObjectReference := &autoscalingv2.CrossVersionObjectReference{}
				internalCrossVersionObjectReference := &kubernetesinternalautoscaling.CrossVersionObjectReference{}
				err = r.Scheme.Convert(hvpa.Spec.TargetRef, internalCrossVersionObjectReference, context.TODO())
				if err != nil {
					return err
				}
				err = r.Scheme.Convert(internalCrossVersionObjectReference, v2CrossVersionObjectReference, context.TODO())
				if err != nil {
					return err
				}
				_, err = r.UpdateHpaV2WithRetries(v2Hpa.Namespace, v2Hpa.Name,
					func(hpaToUpdate *autoscalingv2.HorizontalPodAutoscaler) {
						hpaToUpdate.Spec.MinReplicas = hvpa.Spec.Hpa.Template.Spec.MinReplicas
						hpaToUpdate.Spec.MaxReplicas = hvpa.Spec.Hpa.Template.Spec.MaxReplicas
						hpaToUpdate.Spec.Metrics = v2HpaMetricSpecList
						hpaToUpdate.Spec.ScaleTargetRef = *v2CrossVersionObjectReference
					})
				if err != nil {
					return fmt.Errorf("error in updating hpaTemplateSpec to hpa %q: %v", hpa.Name, err)
				}
				log.V(2).Info("HPA spec sync", "HPA", hpa.Name, "HVPA", hvpa.Namespace+"/"+hvpa.Name)
			}
		}
	}
	return nil
}

func getHpaFromHvpa(hvpa *autoscalingv1alpha1.Hvpa) (*autoscaling.HorizontalPodAutoscaler, error) {
	metadata := hvpa.Spec.Hpa.Template.ObjectMeta.DeepCopy()

	if ownerRef := metadata.GetOwnerReferences(); len(ownerRef) != 0 {
		// TODO: Could be done better as part of validation
		return nil, fmt.Errorf("hpa template in hvpa object already has an owner reference")
	}

	if generateName := metadata.GetGenerateName(); len(generateName) != 0 {
		log.V(3).Info("Warning", "Generate name provided in the hpa template will be ignored", metadata.Namespace+"/"+generateName)
	}

	if name := metadata.GetName(); len(name) != 0 {
		log.V(3).Info("Warning", "Name provided in the hpa template will be ignored", metadata.Namespace+"/"+name)
		metadata.SetName("")
	}

	metadata.SetGenerateName(hvpa.Name + "-")
	metadata.SetNamespace(hvpa.Namespace)

	return &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: *metadata,
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			MaxReplicas:    hvpa.Spec.Hpa.Template.Spec.MaxReplicas,
			MinReplicas:    hvpa.Spec.Hpa.Template.Spec.MinReplicas,
			ScaleTargetRef: *hvpa.Spec.TargetRef.DeepCopy(),
			Metrics:        hvpa.Spec.Hpa.Template.Spec.Metrics,
		},
	}, nil
}
