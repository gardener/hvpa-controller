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
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/util/retry"
)

// TODO: use client library instead when it starts to support update retries
//
//	see https://github.com/kubernetes/kubernetes/issues/21479
type updateVpaFunc func(vpa *vpa_api.VerticalPodAutoscaler)

func (r *HvpaReconciler) claimVpas(hvpa *autoscalingv1alpha1.Hvpa, selector labels.Selector, vpas *vpa_api.VerticalPodAutoscalerList) ([]*vpa_api.VerticalPodAutoscaler, error) {
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
	return cm.ClaimVpas(vpas)
}

// The returned bool value can be used to tell if the vpa is actually updated.
func vpaSpecNeedChange(hvpa *autoscalingv1alpha1.Hvpa, vpa *vpa_api.VerticalPodAutoscaler) bool {
	return !reflect.DeepEqual(hvpa.Spec.Vpa.Template.Spec.ResourcePolicy, vpa.Spec.ResourcePolicy) ||
		*vpa.Spec.UpdatePolicy.UpdateMode != vpa_api.UpdateModeOff
}

// UpdateVpaWithRetries updates a vpa with given applyUpdate function. Note that vpa not found error is ignored.
func (r *HvpaReconciler) UpdateVpaWithRetries(namespace, name string, applyUpdate updateVpaFunc) (*vpa_api.VerticalPodAutoscaler, error) {
	vpa := &vpa_api.VerticalPodAutoscaler{}

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		if err = r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, vpa); err != nil {
			return err
		}
		vpa = vpa.DeepCopy()
		// Apply the update, then attempt to push it to the apiserver.
		applyUpdate(vpa)
		return r.Update(context.TODO(), vpa)
	})

	// Ignore the precondition violated error, this machine is already updated
	// with the desired label.
	if retryErr == errorsutil.ErrPreconditionViolated {
		log.V(4).Info("Vpa %s precondition doesn't hold, skip updating it.", namespace+"/"+name)
		retryErr = nil
	}

	return vpa, retryErr
}

func (r *HvpaReconciler) syncVpaSpec(vpaList []*vpa_api.VerticalPodAutoscaler, hvpa *autoscalingv1alpha1.Hvpa) error {
	for _, vpa := range vpaList {
		if vpaChanged := vpaSpecNeedChange(hvpa, vpa); vpaChanged {
			_, err := r.UpdateVpaWithRetries(vpa.Namespace, vpa.Name,
				func(vpaToUpdate *vpa_api.VerticalPodAutoscaler) {
					vpaToUpdate.Spec.ResourcePolicy = hvpa.Spec.Vpa.Template.Spec.ResourcePolicy
					*vpaToUpdate.Spec.UpdatePolicy.UpdateMode = vpa_api.UpdateModeOff
				})
			if err != nil {
				return fmt.Errorf("error in updating vpaTemplateSpec to vpa %q: %v", vpa.Name, err)
			}
			log.V(2).Info("VPA spec sync", "VPA", vpa.Name, "HVPA", hvpa.Namespace+"/"+hvpa.Name)
		}
	}
	return nil
}

func getVpaFromHvpa(hvpa *autoscalingv1alpha1.Hvpa) (*vpa_api.VerticalPodAutoscaler, error) {
	metadata := hvpa.Spec.Vpa.Template.ObjectMeta.DeepCopy()

	if ownerRef := metadata.GetOwnerReferences(); len(ownerRef) != 0 {
		// TODO: Could be done better as part of validation
		return nil, fmt.Errorf("vpa template in hvpa object already has an owner reference")
	}

	if generateName := metadata.GetGenerateName(); len(generateName) != 0 {
		log.V(3).Info("Warning", "Generate name provided in the vpa template will be ignored", metadata.Namespace+"/"+generateName)
	}

	if name := metadata.GetName(); len(name) != 0 {
		log.V(3).Info("Warning", "Name provided in the hpa template will be ignored", metadata.Namespace+"/"+name)
		metadata.SetName("")
	}

	metadata.SetGenerateName(hvpa.Name + "-")
	metadata.SetNamespace(hvpa.Namespace)

	// Updater policy set to "Off", as we don't want vpa-updater to act on recommendations
	updatePolicy := vpa_api.UpdateModeOff

	return &vpa_api.VerticalPodAutoscaler{
		ObjectMeta: *metadata,
		Spec: vpa_api.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				Name:       hvpa.Spec.TargetRef.Name,
				APIVersion: hvpa.Spec.TargetRef.APIVersion,
				Kind:       hvpa.Spec.TargetRef.Kind,
			},
			ResourcePolicy: hvpa.Spec.Vpa.Template.Spec.ResourcePolicy.DeepCopy(),
			UpdatePolicy: &vpa_api.PodUpdatePolicy{
				UpdateMode: &updatePolicy,
			},
		},
	}, nil
}
