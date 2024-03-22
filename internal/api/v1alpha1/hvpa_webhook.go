// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0


package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var hvpalog = logf.Log.WithName("hvpa-resource")

// SetupWebhookWithManager sets up manager with a new webhook and r as the reconcile.Reconciler
func (r *Hvpa) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-autoscaling-k8s-io-v1alpha1-hvpa,mutating=true,failurePolicy=fail,groups=autoscaling.k8s.io,resources=hvpas,verbs=create;update,versions=v1alpha1,name=mhvpa.kb.io

var _ webhook.Defaulter = &Hvpa{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Hvpa) Default() {
	hvpalog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// +kubebuilder:webhook:path=/validate-autoscaling-k8s-io-v1alpha1-hvpa,mutating=false,failurePolicy=fail,groups=autoscaling.k8s.io,resources=hvpas,verbs=create;update,versions=v1alpha1,name=vhvpa.kb.io

var _ webhook.Validator = &Hvpa{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Hvpa) ValidateCreate() error {
	hvpalog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Hvpa) ValidateUpdate(old runtime.Object) error {
	hvpalog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Hvpa) ValidateDelete() error {
	hvpalog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}
