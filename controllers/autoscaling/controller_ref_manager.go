/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This file was copied and modified from the kubernetes/kubernetes project
https://github.com/kubernetes/kubernetes/release-1.8/pkg/controller/controller_ref_manager.go

Modifications Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
*/

// Package controllers is used to provide the core functionalities of hvpa-controller
package controllers

import (
	"context"
	"fmt"
	"sync"

	//"github.com/gardener/hvpa-controller/apis/autoscaling/v1alpha1"

	autoscaling "k8s.io/api/autoscaling/v2beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// BaseControllerRefManager is the struct is used to identify the base controller of the object
type BaseControllerRefManager struct {
	Controller metav1.Object
	Selector   labels.Selector

	canAdoptErr  error
	canAdoptOnce sync.Once
	CanAdoptFunc func() error
}

// CanAdopt is used to identify if the object can be adopted by the controller
func (m *BaseControllerRefManager) CanAdopt() error {
	m.canAdoptOnce.Do(func() {
		if m.CanAdoptFunc != nil {
			m.canAdoptErr = m.CanAdoptFunc()
		}
	})
	return m.canAdoptErr
}

// ClaimObject tries to take ownership of an object for this controller.
//
// It will reconcile the following:
//   - Adopt orphans if the match function returns true.
//   - Release owned objects if the match function returns false.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The returned boolean indicates whether you now
// own the object.
//
// No reconciliation will be attempted if the controller is being deleted.
func (m *BaseControllerRefManager) ClaimObject(obj metav1.Object, match func(metav1.Object) bool, adopt, release func(metav1.Object) error) (bool, error) {

	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef != nil {
		if controllerRef.UID != m.Controller.GetUID() {
			// Owned by someone else. Ignore.
			return false, nil
		}
		if match(obj) {
			// We already own it and the selector matches.
			// Return true (successfully claimed) before checking deletion timestamp.
			// We're still allowed to claim things we already own while being deleted
			// because doing so requires taking no actions.
			return true, nil
		}
		// Owned by us but selector doesn't match.
		// Try to release, unless we're being deleted.
		if m.Controller.GetDeletionTimestamp() != nil {
			return false, nil
		}
		if err := release(obj); err != nil {
			// If the object no longer exists, ignore the error.
			if errors.IsNotFound(err) {
				return false, nil
			}
			// Either someone else released it, or there was a transient error.
			// The controller should requeue and try again if it's still stale.
			return false, err
		}
		// Successfully released.
		return false, nil
	}

	// It's an orphan.
	if m.Controller.GetDeletionTimestamp() != nil || !match(obj) {
		// Ignore if we're being deleted or selector doesn't match.
		return false, nil
	}

	if obj.GetDeletionTimestamp() != nil {
		// Ignore if the object is being deleted
		return false, nil
	}

	// Selector matches. Try to adopt.
	if err := adopt(obj); err != nil {
		// If the object no longer exists, ignore the error.
		if errors.IsNotFound(err) {
			return false, nil
		}

		// Either someone else claimed it first, or there was a transient error.
		// The controller should requeue and try again if it's still orphaned.
		return false, err
	}
	// Successfully adopted.
	return true, nil
}

// HvpaControllerRefManager is the struct used to manage its child objects
type HvpaControllerRefManager struct {
	BaseControllerRefManager
	controllerKind schema.GroupVersionKind
	reconciler     *HvpaReconciler
}

// NewHvpaControllerRefManager returns a HvpaControllerRefManager that exposes
// methods to manage the controllerRef of its child objects.
//
// The CanAdopt() function can be used to perform a potentially expensive check
// (such as a live GET from the API server) prior to the first adoption.
// It will only be called (at most once) if an adoption is actually attempted.
// If CanAdopt() returns a non-nil error, all adoptions will fail.
//
// NOTE: Once CanAdopt() is called, it will not be called again by the same
//
//	HvpaControllerRefManager HPA/VPA. Create a new HPA/VPA if it makes
//	sense to check CanAdopt() again (e.g. in a different sync pass).
func NewHvpaControllerRefManager(
	reconciler *HvpaReconciler,
	controller metav1.Object,
	selector labels.Selector,
	controllerKind schema.GroupVersionKind,
	canAdopt func() error,
) *HvpaControllerRefManager {
	return &HvpaControllerRefManager{
		BaseControllerRefManager: BaseControllerRefManager{
			Controller:   controller,
			Selector:     selector,
			CanAdoptFunc: canAdopt,
		},
		controllerKind: controllerKind,
		reconciler:     reconciler,
	}
}

// ClaimHpas tries to take ownership of a list of Hpas.
//
// It will reconcile the following:
//   - Adopt orphans if the selector matches.
//   - Release owned objects if the selector no longer matches.
//
// Optional: If one or more filters are specified, a Hpa will only be claimed if
// all filters return true.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The list of Hpas that you now own is returned.
func (m *HvpaControllerRefManager) ClaimHpas(hpas *autoscaling.HorizontalPodAutoscalerList, filters ...func(*autoscaling.HorizontalPodAutoscaler) bool) ([]*autoscaling.HorizontalPodAutoscaler, error) {
	var claimed []*autoscaling.HorizontalPodAutoscaler
	var errlist []error

	match := func(obj metav1.Object) bool {
		hpa := obj.(*autoscaling.HorizontalPodAutoscaler)
		// Check selector first so filters only run on potentially matching Hpas.
		if !m.Selector.Matches(labels.Set(hpa.Labels)) {
			return false
		}
		for _, filter := range filters {
			if !filter(hpa) {
				return false
			}
		}
		return true
	}

	adopt := func(obj metav1.Object) error {
		return m.AdoptHpa(obj.(*autoscaling.HorizontalPodAutoscaler))
	}
	release := func(obj metav1.Object) error {
		return m.ReleaseHpa(obj.(*autoscaling.HorizontalPodAutoscaler))
	}

	for k := range hpas.Items {
		hpa := &hpas.Items[k]
		ok, err := m.ClaimObject(hpa, match, adopt, release)

		if err != nil {
			errlist = append(errlist, err)
			continue
		}
		if ok {
			claimed = append(claimed, hpa)
		}
	}
	return claimed, utilerrors.NewAggregate(errlist)
}

// ClaimVpas tries to take ownership of a list of Vpas.
//
// It will reconcile the following:
//   - Adopt orphans if the selector matches.
//   - Release owned objects if the selector no longer matches.
//
// Optional: If one or more filters are specified, a Vpa will only be claimed if
// all filters return true.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The list of Vpas that you now own is returned.
func (m *HvpaControllerRefManager) ClaimVpas(vpas *vpa_api.VerticalPodAutoscalerList, filters ...func(*vpa_api.VerticalPodAutoscaler) bool) ([]*vpa_api.VerticalPodAutoscaler, error) {
	var claimed []*vpa_api.VerticalPodAutoscaler
	var errlist []error

	match := func(obj metav1.Object) bool {
		vpa := obj.(*vpa_api.VerticalPodAutoscaler)
		// Check selector first so filters only run on potentially matching Vpas.
		if !m.Selector.Matches(labels.Set(vpa.Labels)) {
			return false
		}
		for _, filter := range filters {
			if !filter(vpa) {
				return false
			}
		}
		return true
	}

	adopt := func(obj metav1.Object) error {
		return m.AdoptVpa(obj.(*vpa_api.VerticalPodAutoscaler))
	}
	release := func(obj metav1.Object) error {
		return m.ReleaseVpa(obj.(*vpa_api.VerticalPodAutoscaler))
	}

	for k := range vpas.Items {
		vpa := &vpas.Items[k]
		ok, err := m.ClaimObject(vpa, match, adopt, release)

		if err != nil {
			errlist = append(errlist, err)
			continue
		}
		if ok {
			claimed = append(claimed, vpa)
		}
	}
	return claimed, utilerrors.NewAggregate(errlist)
}

// AdoptHpa sends a patch to take control of the Hpa. It returns the error if
// the patching fails.
func (m *HvpaControllerRefManager) AdoptHpa(hpa *autoscaling.HorizontalPodAutoscaler) error {
	if err := m.CanAdopt(); err != nil {
		return fmt.Errorf("can't adopt hpa %v/%v (%v): %v", hpa.Namespace, hpa.Name, hpa.UID, err)
	}

	hpaClone := hpa.DeepCopy()
	// Note that ValidateOwnerReferences() will reject this patch if another
	// OwnerReference exists with controller=true.
	if err := controllerutil.SetControllerReference(m.Controller, hpaClone, m.reconciler.Scheme); err != nil {
		return err
	}

	return m.reconciler.Patch(context.TODO(), hpaClone, client.MergeFrom(hpa))
}

// ReleaseHpa sends a patch to free the Hpa from the control of the controller.
// It returns the error if the patching fails. 404 and 422 errors are ignored.
func (m *HvpaControllerRefManager) ReleaseHpa(hpa *autoscaling.HorizontalPodAutoscaler) error {
	log.V(4).Info("ReleaseHpa()", "HPA", hpa.Namespace+"/"+hpa.Name, "controller", m.controllerKind.String(), "controller name", m.Controller.GetName())

	hpaClone := hpa.DeepCopy()
	owners := hpaClone.GetOwnerReferences()
	ownersCopy := []metav1.OwnerReference{}
	for i := range owners {
		owner := &owners[i]
		if owner.UID == m.Controller.GetUID() {
			continue
		}
		ownersCopy = append(ownersCopy, *owner)
	}
	if len(ownersCopy) == 0 {
		hpaClone.OwnerReferences = nil
	} else {
		hpaClone.OwnerReferences = ownersCopy
	}

	err := client.IgnoreNotFound(m.reconciler.Patch(context.TODO(), hpaClone, client.MergeFrom(hpa)))
	if errors.IsInvalid(err) {
		// Invalid error will be returned in two cases: 1. the hpa
		// has no owner reference, 2. the uid of the hpa doesn't
		// match, which means the Hpa is deleted and then recreated.
		// In both cases, the error can be ignored.

		// TODO: If the Hpa has owner references, but none of them
		// has the owner.UID, server will silently ignore the patch.
		// Investigate why.
		return nil
	}

	return err
}

// AdoptVpa sends a patch to take control of the Vpa. It returns the error if
// the patching fails.
func (m *HvpaControllerRefManager) AdoptVpa(vpa *vpa_api.VerticalPodAutoscaler) error {
	if err := m.CanAdopt(); err != nil {
		return fmt.Errorf("can't adopt vpa %v/%v (%v): %v", vpa.Namespace, vpa.Name, vpa.UID, err)
	}

	vpaClone := vpa.DeepCopy()
	// Note that ValidateOwnerReferences() will reject this patch if another
	// OwnerReference exists with controller=true.
	if err := controllerutil.SetControllerReference(m.Controller, vpaClone, m.reconciler.Scheme); err != nil {
		return err
	}

	return m.reconciler.Patch(context.TODO(), vpaClone, client.MergeFrom(vpa))
}

// ReleaseVpa sends a patch to free the Vpa from the control of the controller.
// It returns the error if the patching fails. 404 and 422 errors are ignored.
func (m *HvpaControllerRefManager) ReleaseVpa(vpa *vpa_api.VerticalPodAutoscaler) error {
	log.V(4).Info("ReleaseVpa()", "VPA", vpa.Namespace+"/"+vpa.Name, "controller", m.controllerKind.String(), "controller name", m.Controller.GetName())

	vpaClone := vpa.DeepCopy()
	owners := vpaClone.GetOwnerReferences()
	ownersCopy := []metav1.OwnerReference{}
	for i := range owners {
		owner := &owners[i]
		if owner.UID == m.Controller.GetUID() {
			continue
		}
		ownersCopy = append(ownersCopy, *owner)
	}
	if len(ownersCopy) == 0 {
		vpaClone.OwnerReferences = nil
	} else {
		vpaClone.OwnerReferences = ownersCopy
	}

	err := client.IgnoreNotFound(m.reconciler.Patch(context.TODO(), vpaClone, client.MergeFrom(vpa)))
	if errors.IsInvalid(err) {
		// Invalid error will be returned in two cases: 1. the vpa
		// has no owner reference, 2. the uid of the vpa doesn't
		// match, which means the Vpa is deleted and then recreated.
		// In both cases, the error can be ignored.

		// TODO: If the Vpa has owner references, but none of them
		// has the owner.UID, server will silently ignore the patch.
		// Investigate why.
		return nil
	}

	return err
}

// RecheckDeletionTimestamp returns a CanAdopt() function to recheck deletion.
//
// The CanAdopt() function calls getObject() to fetch the latest value,
// and denies adoption attempts if that object has a non-nil DeletionTimestamp.
func RecheckDeletionTimestamp(getObject func() (metav1.Object, error)) func() error {
	return func() error {
		obj, err := getObject()
		if err != nil {
			return fmt.Errorf("can't recheck DeletionTimestamp: %v", err)
		}
		if obj.GetDeletionTimestamp() != nil {
			return fmt.Errorf("%v/%v has just been deleted at %v", obj.GetNamespace(), obj.GetName(), obj.GetDeletionTimestamp())
		}
		return nil
	}
}
