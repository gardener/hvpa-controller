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
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// HvpaReconciler reconciles a Hvpa object
type HvpaReconciler struct {
	client.Client
	Log    logr.Logger
	scheme *runtime.Scheme
}

var log = logf.Log.WithName("controller")

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &HvpaReconciler{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}
func updateEventFunc(e event.UpdateEvent) bool {
	oldPod, ok := e.ObjectOld.(*corev1.Pod)
	if !ok {
		return false
	}

	newPod, ok := e.ObjectNew.(*corev1.Pod)
	if !ok {
		return false
	}

	if isEvictionEvent(oldPod, newPod) || isOomKillEvent(oldPod, newPod) {
		log.V(3).Info("Handle update event for", "pod", newPod.Name)
		return true
	}
	log.V(3).Info("Ignoring update event for", "pod", newPod.Name)
	return false
}

func isEvictionEvent(oldPod, newPod *corev1.Pod) bool {
	if oldPod.Status.Reason == "Evicted" {
		log.V(4).Info("Pod was already evicted")
		return false
	}

	if newPod.Status.Reason == "Evicted" {
		log.V(4).Info("Pod was evicted")
		return true
	}
	return false
}

func isOomKillEvent(oldPod, newPod *corev1.Pod) bool {
	for i := range newPod.Status.ContainerStatuses {
		containerStatus := &newPod.Status.ContainerStatuses[i]
		if containerStatus.RestartCount > 0 &&
			containerStatus.LastTerminationState.Terminated != nil &&
			containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" {

			oldStatus := findStatus(containerStatus.Name, oldPod.Status.ContainerStatuses)
			if oldStatus != nil && containerStatus.RestartCount > oldStatus.RestartCount {
				return true
			}
		}
	}
	return false
}

func findStatus(name string, containerStatuses []corev1.ContainerStatus) *corev1.ContainerStatus {
	for i := range containerStatuses {
		containerStatus := &containerStatuses[i]
		if containerStatus.Name == name {
			return containerStatus
		}
	}
	return nil
}

// OomkillPredicate implements a oomkill predicate function
type OomkillPredicate struct {
	predicate.Funcs
}

type hvpaObj struct {
	Name     string
	Selector labels.Selector
}

var cachedNames map[string][]*hvpaObj
var cacheMux sync.Mutex

//var _ reconcile.Reconciler = &HvapaReconciler{}

// ReconcileHvpa reconciles a Hvpa object
type ReconcileHvpa struct {
	client.Client
	scheme *runtime.Scheme
}

const deleteFinalizerName = "autoscaling.k8s.io/hvpa-controller"

func (r *HvpaReconciler) getSelectorFromHvpa(instance *autoscalingv1alpha1.Hvpa) (labels.Selector, error) {
	targetRef := instance.Spec.TargetRef

	target := &unstructured.Unstructured{}
	target.SetAPIVersion(targetRef.APIVersion)
	target.SetKind(targetRef.Kind)

	err := r.Get(context.TODO(), types.NamespacedName{Name: targetRef.Name, Namespace: instance.Namespace}, target)
	if err != nil {
		log.Error(err, "Error getting target using targetRef.", "Will skip", instance.Name)
		return nil, err
	}

	selectorMap, found, err := unstructured.NestedMap(target.Object, "spec", "selector")
	if err != nil {
		log.Error(err, "Not able to get the selectorMap from target.", "Will skip", instance.Name)
		return nil, err
	}
	if !found {
		log.V(2).Info("Target doesn't have selector", "will skip HVPA", instance.Name)
		return nil, err
	}

	labelSelector := &metav1.LabelSelector{}
	selectorStr, err := json.Marshal(selectorMap)
	err = json.Unmarshal(selectorStr, &labelSelector)
	if err != nil {
		log.Error(err, "Error in reading selector string.", "will skip", instance.Name)
		return nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", instance.Name)
		return nil, err
	}
	return selector, nil
}

// manageCache manages the global map of HVPAs
func (r *HvpaReconciler) manageCache(instance *autoscalingv1alpha1.Hvpa) {
	selector, err := r.getSelectorFromHvpa(instance)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", instance.Name)
		return
	}

	obj := hvpaObj{
		Name:     instance.Name,
		Selector: selector,
	}

	cacheMux.Lock()
	defer cacheMux.Unlock()

	if _, ok := cachedNames[instance.Namespace]; !ok {
		if cachedNames == nil {
			cachedNames = make(map[string][]*hvpaObj)
		}
		cachedNames[instance.Namespace] = make([]*hvpaObj, 1)
		cachedNames[instance.Namespace] = []*hvpaObj{&obj}
	} else {
		found := false
		for i, cache := range cachedNames[instance.Namespace] {
			if cache.Name == obj.Name {
				found = true
				if instance.DeletionTimestamp != nil {
					// object is under deletion, remove it from the cache
					len := len(cachedNames[instance.Namespace])
					cachedNames[instance.Namespace][i] = cachedNames[instance.Namespace][len-1]
					cachedNames[instance.Namespace][len-1] = nil
					cachedNames[instance.Namespace] = cachedNames[instance.Namespace][:len-1]
					log.V(3).Info("HVPA", instance.Name, "removed from cache")
				}
				break
			}
		}
		if !found {
			cachedNames[instance.Namespace] = append(cachedNames[instance.Namespace], &obj)
		}
	}
	log.V(4).Info("HVPA", "number of hvpas in cache", len(cachedNames[instance.Namespace]), "for namespace", instance.Namespace)
}

func (r *HvpaReconciler) addHvpaFinalizers(hvpa *autoscalingv1alpha1.Hvpa) {
	clone := hvpa.DeepCopy()

	if finalizers := sets.NewString(clone.Finalizers...); !finalizers.Has(deleteFinalizerName) {
		finalizers.Insert(deleteFinalizerName)
		r.updateFinalizers(clone, finalizers.List())
	}
}

func (r *HvpaReconciler) updateFinalizers(hvpa *autoscalingv1alpha1.Hvpa, finalizers []string) {
	// Get the latest version of the machine so that we can avoid conflicts
	instance := &autoscalingv1alpha1.Hvpa{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: hvpa.Name, Namespace: hvpa.Namespace}, instance)
	if err != nil {
		return
	}

	clone := instance.DeepCopy()
	clone.Finalizers = finalizers
	err = r.Update(context.TODO(), clone)
	if err != nil {
		// Free the memory for clone before retrying, so that we limit memory usage in case we keep failing
		clone = nil
		// Keep retrying until update goes through
		log.V(2).Info("Warning: Update failed, retrying")
		r.updateFinalizers(hvpa, finalizers)
	}
}

func (r *HvpaReconciler) deleteHvpaFinalizers(hvpa *autoscalingv1alpha1.Hvpa) {
	clone := hvpa.DeepCopy()

	if finalizers := sets.NewString(clone.Finalizers...); finalizers.Has(deleteFinalizerName) {
		finalizers.Delete(deleteFinalizerName)
		r.updateFinalizers(clone, finalizers.List())
	}
}

func getVpaFromHvpa(hvpa *autoscalingv1alpha1.Hvpa) *vpa_api.VerticalPodAutoscaler {
	// Updater policy set to "Off", as we don't want vpa-updater to act on recommendations
	updatePolicy := vpa_api.UpdateModeOff

	return &vpa_api.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hvpa.Name + "-vpa",
			Namespace: hvpa.Namespace,
		},
		Spec: vpa_api.VerticalPodAutoscalerSpec{
			TargetRef: &autoscalingv1.CrossVersionObjectReference{
				Name:       hvpa.Spec.TargetRef.Name,
				APIVersion: hvpa.Spec.TargetRef.APIVersion,
				Kind:       hvpa.Spec.TargetRef.Kind,
			},
			ResourcePolicy: hvpa.Spec.VpaTemplate.ResourcePolicy.DeepCopy(),
			UpdatePolicy: &vpa_api.PodUpdatePolicy{
				UpdateMode: &updatePolicy,
			},
		},
	}
}

func (r *HvpaReconciler) reconcileVpa(hvpa *autoscalingv1alpha1.Hvpa) (*vpa_api.VerticalPodAutoscalerStatus, error) {
	vpa := getVpaFromHvpa(hvpa)

	if err := controllerutil.SetControllerReference(hvpa, vpa, r.scheme); err != nil {
		return nil, err
	}

	foundVpa := &vpa_api.VerticalPodAutoscaler{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: vpa.Name, Namespace: vpa.Namespace}, foundVpa)
	if err != nil && errors.IsNotFound(err) {
		log.V(2).Info("Creating VPA", "namespace", vpa.Namespace, "name", vpa.Name)
		err = r.Create(context.TODO(), vpa)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	// Update the found object and write the result back if there are any changes
	if !reflect.DeepEqual(vpa.Spec, foundVpa.Spec) {
		foundVpa.Spec = vpa.Spec
		log.V(2).Info("Updating VPA", "namespace", vpa.Namespace, "name", vpa.Name)
		err = r.Update(context.TODO(), foundVpa)
		if err != nil {
			return nil, err
		}
	}

	status := foundVpa.Status.DeepCopy()

	return status, nil
}

func (r *HvpaReconciler) reconcileHpa(hvpa *autoscalingv1alpha1.Hvpa) (*autoscaling.HorizontalPodAutoscalerStatus, error) {
	hpa := &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hvpa.Name + "-hpa",
			Namespace: hvpa.Namespace,
		},
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			MaxReplicas:    hvpa.Spec.HpaTemplate.MaxReplicas,
			MinReplicas:    hvpa.Spec.HpaTemplate.MinReplicas,
			ScaleTargetRef: *hvpa.Spec.TargetRef.DeepCopy(),
			Metrics:        hvpa.Spec.HpaTemplate.Metrics,
		},
	}

	anno := hvpa.GetAnnotations()
	if val, ok := anno["hpa-controller"]; ok && val == "hvpa" {
		// If this annotation is set on hvpa, AND
		// If the value of this annotation on hvpa is "hvpa", then set hpa's mode off
		// so that kube-controller-manager doesn't act on hpa recommendations
		annotations := make(map[string]string)
		annotations["mode"] = "Off"

		hpa.SetAnnotations(annotations)
	}

	if err := controllerutil.SetControllerReference(hvpa, hpa, r.scheme); err != nil {
		return nil, err
	}

	foundHpa := &autoscaling.HorizontalPodAutoscaler{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, foundHpa)
	if err != nil && errors.IsNotFound(err) {
		log.V(2).Info("Creating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
		err = r.Create(context.TODO(), hpa)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	// Update the found object and write the result back if there are any changes
	if !reflect.DeepEqual(hpa.Spec, foundHpa.Spec) || !reflect.DeepEqual(hpa.GetAnnotations(), foundHpa.GetAnnotations()) {
		foundHpa.Spec = hpa.Spec
		foundHpa.SetAnnotations(hpa.GetAnnotations())
		log.V(2).Info("Updating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
		err = r.Update(context.TODO(), foundHpa)
		if err != nil {
			return nil, err
		}
	}

	status := foundHpa.Status.DeepCopy()
	return status, nil
}

func getVpaWeightFromIntervals(hvpa *autoscalingv1alpha1.Hvpa, desiredReplicas, currentReplicas int32) autoscalingv1alpha1.VpaWeight {
	var vpaWeight autoscalingv1alpha1.VpaWeight
	// lastFraction is set to default 1 to handle the case when vpaWeight is 1 in the matching interval,
	// and there are no fractional vpaWeights in the previous intervals. So we need to default to this value
	lastFraction := autoscalingv1alpha1.VpaWeight(1)
	lookupNextFraction := false
	for _, interval := range hvpa.Spec.WeightBasedScalingIntervals {
		if lookupNextFraction {
			if interval.VpaWeight < 1 {
				vpaWeight = interval.VpaWeight
				break
			} else {
				continue
			}
		}
		// TODO: Following 2 if checks need to be done as part of verification process
		if interval.StartReplicaCount == 0 {
			interval.StartReplicaCount = *hvpa.Spec.HpaTemplate.MinReplicas
		}
		if interval.LastReplicaCount == 0 {
			interval.LastReplicaCount = hvpa.Spec.HpaTemplate.MaxReplicas
		}
		if interval.VpaWeight < 1 {
			lastFraction = interval.VpaWeight
		}
		if currentReplicas >= interval.StartReplicaCount && currentReplicas <= interval.LastReplicaCount {
			vpaWeight = interval.VpaWeight
			if vpaWeight == 1 {
				if desiredReplicas < currentReplicas {
					// If HPA wants to scale in, use last seen fractional value as vpaWeight
					// If there is no such value, we cannot scale in anyway, so keep it default 1
					vpaWeight = lastFraction
				} else if desiredReplicas > currentReplicas {
					// If HPA wants to scale out, use next fractional value as vpaWeight
					// If there is no such value, we can not scale out anyway, so we will end up with vpaWeight = 1
					lookupNextFraction = true
					continue
				}
			}
			break
		}
	}
	return vpaWeight
}

func (r *HvpaReconciler) scaleIfRequired(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, target runtime.Object) (int32, bool, autoscalingv1alpha1.VpaWeight, error) {

	var newObj runtime.Object
	var deploy *appsv1.Deployment
	var ss *appsv1.StatefulSet
	var ds *appsv1.DaemonSet
	var rs *appsv1.ReplicaSet
	var rc *corev1.ReplicationController
	var currentReplicas int32
	var podSpec *corev1.PodSpec

	kind := target.GetObjectKind().GroupVersionKind().Kind
	targetCopy := target.DeepCopyObject()

	switch kind {
	case "Deployment":
		deploy = targetCopy.(*appsv1.Deployment)
		currentReplicas = *deploy.Spec.Replicas
		podSpec = &deploy.Spec.Template.Spec
	case "StatefulSet":
		ss = targetCopy.(*appsv1.StatefulSet)
		currentReplicas = *ss.Spec.Replicas
		podSpec = &ss.Spec.Template.Spec
	case "DaemonSet":
		ds = targetCopy.(*appsv1.DaemonSet)
		podSpec = &ds.Spec.Template.Spec
	case "ReplicaSet":
		rs = targetCopy.(*appsv1.ReplicaSet)
		currentReplicas = *rs.Spec.Replicas
		podSpec = &rs.Spec.Template.Spec
	case "ReplicationController":
		rc = targetCopy.(*corev1.ReplicationController)
		currentReplicas = *rc.Spec.Replicas
		podSpec = &rc.Spec.Template.Spec
	default:
		err := fmt.Errorf("TargetRef kind not supported %v", kind)
		log.Error(err, "Error")
		return 0, false, 0, err
	}

	var desiredReplicas int32
	if hpaStatus == nil {
		desiredReplicas = currentReplicas
	} else {
		desiredReplicas = hpaStatus.DesiredReplicas
	}

	vpaWeight := getVpaWeightFromIntervals(hvpa, desiredReplicas, currentReplicas)

	hpaScaleOutLimited := isHpaScaleOutLimited(hpaStatus, hvpa.Spec.HpaTemplate.MaxReplicas)

	// Memory for newDeploy is assigned in the function getWeightedRequests
	newPodSpec, resourceChanged, err := getWeightedRequests(vpaStatus, hvpa, vpaWeight, podSpec, hpaScaleOutLimited)
	if err != nil {
		log.Error(err, "Error in getting weight based requests in new deployment")
	}

	weightedReplicas, err := getWeightedReplicas(hpaStatus, hvpa, currentReplicas, 1-vpaWeight)
	if err != nil {
		log.Error(err, "Error in getting weight based replicas")
	}

	if (weightedReplicas == 0 || currentReplicas == weightedReplicas) &&
		(newPodSpec == nil || reflect.DeepEqual(podSpec, newPodSpec)) {
		log.V(3).Info("Scaling not required")
		return 0, false, vpaWeight, nil
	}

	if weightedReplicas == 0 {
		weightedReplicas = currentReplicas
	}

	switch kind {
	case "Deployment":
		deploy.Spec.Replicas = &weightedReplicas
		if newPodSpec != nil {
			newPodSpec.DeepCopyInto(&deploy.Spec.Template.Spec)
		}
		newObj = deploy
	case "StatefulSet":
		ss.Spec.Replicas = &weightedReplicas
		if newPodSpec != nil {
			newPodSpec.DeepCopyInto(&ss.Spec.Template.Spec)
		}
		newObj = ss
	case "DaemonSet":
		if newPodSpec != nil {
			newPodSpec.DeepCopyInto(&ds.Spec.Template.Spec)
		}
		newObj = ds
	case "ReplicaSet":
		rs.Spec.Replicas = &weightedReplicas
		if newPodSpec != nil {
			newPodSpec.DeepCopyInto(&rs.Spec.Template.Spec)
		}
		newObj = rs
	case "ReplicationController":
		rc.Spec.Replicas = &weightedReplicas
		if newPodSpec != nil {
			newPodSpec.DeepCopyInto(&rc.Spec.Template.Spec)
		}
		newObj = rc
	default:
		err := fmt.Errorf("TargetRef kind not supported %v", kind)
		log.Error(err, "Error")
		return 0, false, vpaWeight, err
	}

	log.V(3).Info("Scaling required")
	return weightedReplicas - currentReplicas, resourceChanged, vpaWeight, r.Update(context.TODO(), newObj)
}

func getWeightedReplicas(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, currentReplicas int32, hpaWeight autoscalingv1alpha1.VpaWeight) (int32, error) {
	anno := hvpa.GetAnnotations()
	if val, ok := anno["hpa-controller"]; !ok || val != "hvpa" {
		log.V(3).Info("HPA is not controlled by HVPA")
		return 0, nil
	}

	log.V(2).Info("Calculating weighted replicas", "hpaWeight", hpaWeight)
	if hpaWeight == 0 || hpaStatus == nil || hpaStatus.DesiredReplicas == 0 {
		log.V(2).Info("HPA: Nothing to do")
		return 0, nil
	}

	var err error
	var weightedReplicas int32
	desiredReplicas := hpaStatus.DesiredReplicas

	if desiredReplicas == currentReplicas {
		log.V(2).Info("HPA", "no scaling required. Current replicas", currentReplicas)
		return currentReplicas, err
	}

	if desiredReplicas > currentReplicas {
		weightedReplicas = int32(math.Ceil(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)))
	} else {
		weightedReplicas = int32(math.Floor(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)))
	}

	if weightedReplicas == currentReplicas {
		log.V(2).Info("HPA", "no scaling required. Weighted replicas", weightedReplicas)
		return currentReplicas, err
	}

	lastScaleTime := hvpa.Status.HvpaStatus.LastScaleTime.DeepCopy()
	overrideScaleUpStabilization := hvpa.Status.HvpaStatus.OverrideScaleUpStabilization
	if overrideScaleUpStabilization {
		log.V(2).Info("HPA", "will override last scale time in case of scale out", overrideScaleUpStabilization)
	}
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := metav1.Now().Sub(lastScaleTime.Time)
	scaleUpStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleUpStabilization.Duration)
	scaleDownStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleDownStabilization.Duration)

	if weightedReplicas > currentReplicas && (overrideScaleUpStabilization || lastScaleTimeDuration > scaleUpStabilizationWindow) {
		log.V(2).Info("HPA scaling up", "weighted replicas", weightedReplicas)
		return weightedReplicas, err
	} else if weightedReplicas < currentReplicas && lastScaleTimeDuration > scaleDownStabilizationWindow {
		log.V(2).Info("HPA scaling down", "weighted replicas", weightedReplicas)
		return weightedReplicas, err
	}

	log.V(2).Info("HPA: Not scaling as hvpa is in stabilization window", "currentReplicas", currentReplicas, "weightedReplicas", weightedReplicas, "minutes after last scaling", lastScaleTimeDuration.Minutes())
	return currentReplicas, err
}

func isHpaScaleOutLimited(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, maxReplicas int32) bool {
	if hpaStatus == nil || hpaStatus.Conditions == nil {
		return false
	}
	if hpaStatus.DesiredReplicas < maxReplicas {
		return false
	}
	for _, v := range hpaStatus.Conditions {
		if v.Type == autoscaling.ScalingLimited && v.Status == corev1.ConditionTrue {
			log.V(2).Info("HPA scale out is limited")
			return true
		}
	}
	return false
}

func isScaleDownEnabled(hvpa *autoscalingv1alpha1.Hvpa) bool {
	anno := hvpa.GetAnnotations()
	if val, ok := anno["enable-vertical-scale-down"]; ok && val == "true" {
		return true
	}
	return false
}

// getWeightedRequests returns updated copy of podSpec if there is any change in podSpec,
// otherwise it returns nil
func getWeightedRequests(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, vpaWeight autoscalingv1alpha1.VpaWeight, podSpec *corev1.PodSpec, hpaScaleOutLimited bool) (*corev1.PodSpec, bool, error) {
	log.V(2).Info("Checking if need to scale vertically")
	if vpaWeight == 0 || vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.V(2).Info("VPA: Nothing to do")
		return nil, false, nil
	}
	for k, v := range vpaStatus.Conditions {
		if v.Type == vpa_api.RecommendationProvided {
			if v.Status == "True" {
				// VPA recommendations are provided, we can do further processing
				break
			} else {
				log.V(2).Info("VPA recommendations not provided yet")
				return nil, false, nil
			}
		}
		if k == len(vpaStatus.Conditions)-1 {
			log.V(2).Info("Reliable VPA recommendations not provided yet")
			return nil, false, nil
		}
	}
	recommendations := vpaStatus.Recommendation

	lastScaleTime := hvpa.Status.HvpaStatus.LastScaleTime
	overrideScaleUpStabilization := hvpa.Status.HvpaStatus.OverrideScaleUpStabilization
	if overrideScaleUpStabilization {
		log.V(2).Info("VPA", "will override last scale time in case of scale up", overrideScaleUpStabilization)
	}
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := time.Now().Sub(lastScaleTime.Time)
	scaleUpStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleUpStabilization.Duration)
	scaleDownStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleDownStabilization.Duration)

	scaleDownEnaled := isScaleDownEnabled(hvpa)

	resourceChange := false
	newPodSpec := podSpec.DeepCopy()

	for _, rec := range recommendations.ContainerRecommendations {
		for id, container := range newPodSpec.Containers {
			if rec.ContainerName == container.Name {
				vpaMemTarget := rec.Target.Memory().DeepCopy()
				vpaCPUTarget := rec.Target.Cpu().DeepCopy()
				currMem := newPodSpec.Containers[id].Resources.Requests.Memory().DeepCopy()
				currCPU := newPodSpec.Containers[id].Resources.Requests.Cpu().DeepCopy()

				log.V(2).Info("VPA", "target mem", vpaMemTarget, "target cpu", vpaCPUTarget, "vpaWeight", vpaWeight, "minutes after last scaling", lastScaleTimeDuration.Minutes())

				// Scale vpaWeight by a factor of 100, otherwise
				// values such as 200m will become 0 as all calculations are done using int64
				factor := int64(100)
				scale := int64(float64(vpaWeight) * float64(factor))

				scaleUpMinDeltaMem, _ := getThreshold(hvpa.Spec.ScaleUpStabilization.MinMemChange, corev1.ResourceMemory, currMem)
				scaleDownMinDeltaMem, _ := getThreshold(hvpa.Spec.ScaleDownStabilization.MinMemChange, corev1.ResourceMemory, currMem)
				vpaMemTarget.Sub(currMem)
				diffMem := resource.NewQuantity(vpaMemTarget.Value()*scale/factor, vpaMemTarget.Format)
				negDiffMem := resource.NewQuantity(-vpaMemTarget.Value()*scale/factor, vpaMemTarget.Format)
				currMem.Add(*diffMem)
				weightedMem := currMem

				scaleUpMinDeltaCPU, _ := getThreshold(hvpa.Spec.ScaleUpStabilization.MinCPUChange, corev1.ResourceCPU, currCPU)
				scaleDownMinDeltaCPU, _ := getThreshold(hvpa.Spec.ScaleDownStabilization.MinCPUChange, corev1.ResourceCPU, currCPU)
				vpaCPUTarget.Sub(currCPU)
				diffCPU := resource.NewQuantity(vpaCPUTarget.ScaledValue(-3)*scale/factor, vpaCPUTarget.Format)
				negDiffCPU := resource.NewQuantity(-vpaCPUTarget.ScaledValue(-3)*scale/factor, vpaCPUTarget.Format)
				diffCPU.SetScaled(diffCPU.Value(), -3)
				currCPU.Add(*diffCPU)
				weightedCPU := currCPU

				log.V(3).Info("VPA", "weighted target mem", weightedMem, "weighted target cpu", weightedCPU)
				log.V(3).Info("VPA sclae down", "minimum CPU delta", scaleDownMinDeltaCPU.String(), "minimum memory delta", scaleDownMinDeltaMem, "scale down enabled", scaleDownEnaled)
				log.V(3).Info("VPA scale up", "minimum CPU delta", scaleUpMinDeltaCPU.String(), "minimum memory delta", scaleUpMinDeltaMem, "HPA condition ScalingLimited", hpaScaleOutLimited)

				if hpaScaleOutLimited && diffMem.Sign() > 0 && diffMem.Cmp(*scaleUpMinDeltaMem) > 0 &&
					(overrideScaleUpStabilization || lastScaleTimeDuration > scaleUpStabilizationWindow) {
					// If the difference is greater than minimum delta, AND
					// scale stabilization window is expired
					log.V(2).Info("VPA", "Scaling up", "memory", "Container", container.Name)
					newPodSpec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
					resourceChange = true
				} else if scaleDownEnaled && diffMem.Sign() < 0 && negDiffMem.Cmp(*scaleDownMinDeltaMem) > 0 &&
					lastScaleTimeDuration > scaleDownStabilizationWindow {
					log.V(2).Info("VPA", "Scaling down", "memory", "Container", container.Name)
					newPodSpec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
					resourceChange = true
				}

				if hpaScaleOutLimited && diffCPU.Sign() > 0 && diffCPU.Cmp(*scaleUpMinDeltaCPU) > 0 &&
					(overrideScaleUpStabilization || lastScaleTimeDuration > scaleUpStabilizationWindow) {
					// If the difference is greater than minimum delta, AND
					// scale stabilization window is expired
					log.V(2).Info("VPA", "Scaling up", "CPU", "Container", container.Name)
					newPodSpec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
					resourceChange = true
				} else if scaleDownEnaled && diffCPU.Sign() < 0 && negDiffCPU.Cmp(*scaleDownMinDeltaCPU) > 0 &&
					lastScaleTimeDuration > scaleDownStabilizationWindow {
					log.V(2).Info("VPA", "Scaling down", "CPU", "Container", container.Name)
					newPodSpec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
					resourceChange = true
				}

				// TODO: Add conditions for other resources also: ResourceStorage, ResourceEphemeralStorage,
				break
			}
		}
	}
	log.V(2).Info("VPA", "vpa recommends changes?", resourceChange)
	if resourceChange {
		return newPodSpec, resourceChange, nil
	}
	return nil, false, nil
}

func getThreshold(thresholdVals *autoscalingv1alpha1.ChangeThreshold, resourceType corev1.ResourceName, currentVal resource.Quantity) (*resource.Quantity, error) {
	const (
		defaultMemThreshold string = "200M"
		defaultCPUThreshold string = "200m"
	)
	var quantity resource.Quantity
	if thresholdVals == nil {
		if resourceType == corev1.ResourceMemory {
			// Set to default
			quantity = resource.MustParse(defaultMemThreshold)
		}
		if resourceType == corev1.ResourceCPU {
			// Set to default
			quantity = resource.MustParse(defaultCPUThreshold)
		}
		return &quantity, nil
	}

	if thresholdVals.Percentage == nil {
		quantity = resource.MustParse(*thresholdVals.Value)
		return &quantity, nil
	}

	percentageValue := currentVal.ScaledValue(-3) * int64(*thresholdVals.Percentage) / 100
	percentageQuantity := resource.NewQuantity(percentageValue, currentVal.Format)
	percentageQuantity.SetScaled(percentageQuantity.Value(), -3)

	if thresholdVals.Value == nil {
		return percentageQuantity, nil
	}

	absoluteQuantity := resource.MustParse(*thresholdVals.Value)
	if percentageQuantity.Cmp(absoluteQuantity) < 0 {
		return percentageQuantity, nil
	}
	return &absoluteQuantity, nil
}

// Reconcile reads that state of the cluster for a Hvpa object and makes changes based on the state read
// and what is in the Hvpa.Spec
// Automatically generate RBAC rules to allow the Controller to read and write HPAs and VPAs
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=pods;replicationcontrollers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets;replicasets;statefulsets;deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=hvpas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=hvpas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;watch;list

func (r *HvpaReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("hvpa", req.NamespacedName)

	// Fetch the Hvpa instance
	instance := &autoscalingv1alpha1.Hvpa{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	log.V(1).Info("Reconciling", "hvpa", instance.GetName())

	r.manageCache(instance)

	if instance.GetDeletionTimestamp() != nil {
		if finalizers := sets.NewString(instance.Finalizers...); finalizers.Has(deleteFinalizerName) {
			r.deleteHvpaFinalizers(instance)
		}

		return ctrl.Result{}, r.Delete(context.TODO(), instance)
	}

	r.addHvpaFinalizers(instance)

	// Default duration after which the object should be requeued
	requeAfter, _ := time.ParseDuration("1m")
	result := ctrl.Result{
		RequeueAfter: requeAfter,
	}

	hpaStatus, err := r.reconcileHpa(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	vpaStatus, err := r.reconcileVpa(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	var obj runtime.Object
	switch instance.Spec.TargetRef.Kind {
	case "Deployment":
		obj = &appsv1.Deployment{}
	case "StatefulSet":
		obj = &appsv1.StatefulSet{}
	case "DaemonSet":
		obj = &appsv1.DaemonSet{}
	case "ReplicaSet":
		obj = &appsv1.ReplicaSet{}
	case "ReplicationController":
		obj = &corev1.ReplicationController{}
	default:
		err := fmt.Errorf("TargetRef kind not supported %v", instance.Spec.TargetRef.Kind)
		log.Error(err, "Error")
		// Don't return error, and requeue, so that reconciliation is tried after default sync period only
		return ctrl.Result{}, nil
	}

	err = r.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.TargetRef.Name, Namespace: instance.Namespace}, obj)
	if err != nil {
		log.Error(err, "Error getting", "kind", instance.Spec.TargetRef.Kind, "name", instance.Spec.TargetRef.Name, "namespace", instance.Namespace)
		return ctrl.Result{}, err
	}

	hpaScaled, vpaScaled, vpaWeight, err := r.scaleIfRequired(hpaStatus, vpaStatus, instance, obj)
	if err != nil {
		return ctrl.Result{}, err
	}

	hvpa := instance.DeepCopy()
	if hpaStatus != nil {
		hvpa.Status.HpaStatus.CurrentReplicas = hpaStatus.CurrentReplicas
		hvpa.Status.HpaStatus.DesiredReplicas = hpaStatus.DesiredReplicas
	}
	if vpaStatus != nil {
		hvpa.Status.VpaStatus.Recommendation = vpaStatus.Recommendation.DeepCopy()
	}
	if hpaScaled != 0 || vpaScaled {
		now := metav1.Now()
		hvpa.Status.HvpaStatus.LastScaleTime = &now
		hvpa.Status.HvpaStatus.HpaWeight = 1 - vpaWeight
		hvpa.Status.HvpaStatus.VpaWeight = vpaWeight
		if hpaScaled > 0 {
			hvpa.Status.HvpaStatus.LastScaleType.Horizontal = autoscalingv1alpha1.Out
		} else if hpaScaled < 0 {
			hvpa.Status.HvpaStatus.LastScaleType.Horizontal = autoscalingv1alpha1.In
		}
		hvpa.Status.HvpaStatus.OverrideScaleUpStabilization = false
		/*// As only scale up is implemented yet
		if vpaScaled {
			hvpa.Status.HvpaStatus.LastScaleType.Vertical = autoscalingv1alpha1.Up
		}*/
	}
	if !reflect.DeepEqual(hvpa.Status, instance.Status) {
		return result, r.Update(context.TODO(), hvpa)
	}

	return result, nil

	//	return ctrl.Result{}, nil
}

func (r *HvpaReconciler) SetupWithManager(mgr ctrl.Manager) error {

	hvpaSource := source.Kind{Type: &autoscalingv1alpha1.Hvpa{}}
	hvpaHandler := handler.EnqueueRequestForObject{}

	hpaSource := source.Kind{Type: &autoscaling.HorizontalPodAutoscaler{}}
	hpaHandler := handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &autoscalingv1alpha1.Hvpa{},
	}

	vpaSource := source.Kind{Type: &vpa_api.VerticalPodAutoscaler{}}
	vpaHandler := handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &autoscalingv1alpha1.Hvpa{},
	}

	eventHandler := &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
			/* This event handler function, sets the flag on hvpa to override the last scale time stabilization window if:
			 * 1. The pod was oomkilled, OR
			 * 2. The pod was evicted, and the node was under memory pressure
			 */
			pod := a.Object.(*corev1.Pod)
			nodeName := pod.Spec.NodeName
			if nodeName == "" {
				return nil
			}
			client := mgr.GetClient()

			// Get HVPA from the cache
			name := ""
			for _, obj := range cachedNames[pod.Namespace] {
				if obj.Selector.Matches(labels.Set(pod.GetLabels())) {
					name = obj.Name
					break
				}
			}
			if name == "" {
				// HVPA object not found for the pod
				return nil
			}

			log.V(4).Info("Checking if need to override last scale time.", "hvpa", name, "pod", pod.Name)
			// Get latest HVPA object
			hvpa := &autoscalingv1alpha1.Hvpa{}
			err := client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Meta.GetNamespace()}, hvpa)
			if err != nil {
				log.Error(err, "Error retreiving hvpa")
				return nil
			}
			clone := hvpa.DeepCopy()

			hvpaStatus := clone.Status.HvpaStatus
			if hvpaStatus.LastScaleTime == nil || hvpaStatus.OverrideScaleUpStabilization == true {
				log.V(4).Info("HVPA status already set to override last scale time.")
				return nil
			}

			if pod.Status.Reason == "Evicted" {
				// If pod was evicted beause of 'KubeletHasInsufficientMemory' node condition,
				// only then we want to continue, otherwise exit
				req := types.NamespacedName{
					Name: nodeName,
				}
				node := corev1.Node{}
				err := client.Get(context.TODO(), req, &node)
				if err != nil {
					log.Error(err, "Error fetching node")
					return nil
				}

				hasMemPressure := false
				for _, condition := range node.Status.Conditions {
					if condition.Reason == "KubeletHasInsufficientMemory" && condition.Status == corev1.ConditionTrue {
						hasMemPressure = true
						break
					}
				}
				if !hasMemPressure {
					log.V(4).Info("Pod was evicted, but the node is not under memory pressure.")
					return nil
				}
				log.V(4).Info("Pod might have been evited because node was under memory pressure")
			} else {
				// The pod was oomkilled
				// Check if scaling already happened after this was oomkilled
				recent := false
				for i := range pod.Status.ContainerStatuses {
					containerStatus := &pod.Status.ContainerStatuses[i]
					if containerStatus.RestartCount > 0 &&
						containerStatus.LastTerminationState.Terminated != nil &&
						containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" &&
						clone.Status.HvpaStatus.LastScaleTime != nil &&
						containerStatus.LastTerminationState.Terminated.FinishedAt.After(clone.Status.HvpaStatus.LastScaleTime.Time) {

						recent = true
						break
					}
				}
				if !recent {
					log.V(4).Info("This is not  a recent oomkill. Return")
					return nil
				}
			}

			clone.Status.HvpaStatus.OverrideScaleUpStabilization = true

			log.V(2).Info("Updating HVPA status to override last scale time", "HVPA", clone.Name)
			err = client.Update(context.TODO(), clone)
			if err != nil {
				log.Error(err, "Error overrinding last scale time for", "HVPA", name)
			}

			return nil
		}),
	}

	pred := OomkillPredicate{
		Funcs: predicate.Funcs{
			UpdateFunc: updateEventFunc,
		},
	}

	podSource := source.Kind{Type: &corev1.Pod{}}
	ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.Hvpa{}).
		Watches(&hvpaSource, &hvpaHandler).
		Watches(&hpaSource, &hpaHandler).
		Watches(&vpaSource, &vpaHandler).
		Watches(&podSource, eventHandler).WithEventFilter(pred).Complete(r)

	return nil

}
