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

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
)

// TODO: use client library instead when it starts to support update retries
//
//	see https://github.com/kubernetes/kubernetes/issues/21479
type updateHpaFunc func(hpa *autoscaling.HorizontalPodAutoscaler)

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

func (r *HvpaReconciler) syncHpaSpec(hpaList []*autoscaling.HorizontalPodAutoscaler, hvpa *autoscalingv1alpha1.Hvpa) error {
	for _, hpa := range hpaList {
		if hpaChanged := hpaSpecNeedChange(hvpa, hpa); hpaChanged {
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
