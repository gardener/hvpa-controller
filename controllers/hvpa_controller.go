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

	autoscalingv1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	validation "github.com/gardener/hvpa-controller/api/validation"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var controllerKindHvpa = autoscalingv1alpha1.SchemeGroupVersionHvpa.WithKind("Hvpa")

// HvpaReconciler reconciles a Hvpa object
type HvpaReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	EnableDetailedMetrics bool
	metrics               *hvpaMetrics
}

var log = logf.Log.WithName("controller").WithName("hvpa")

func updateEventFunc(e event.UpdateEvent) bool {
	// If update event is for HVPA/HPA/VPA then we would want to reconcile unconditionally.
	switch t := e.ObjectOld.(type) {
	case *autoscalingv1alpha1.Hvpa, *autoscaling.HorizontalPodAutoscaler, *vpa_api.VerticalPodAutoscaler:
		log.V(4).Info("Update event for", "kind", t.GetObjectKind().GroupVersionKind().Kind)
		return true
	case *corev1.Pod:
		log.V(4).Info("Update event for pod")
	default:
		log.V(4).Info("Update event of an un-managed resource")
		return false
	}

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

// removeFromCache removes hvpa from the internal cache. The caller is responsible for synchronisation using cacheMux.
func removeFromCache(namespacedName types.NamespacedName) {
	for i, cache := range cachedNames[namespacedName.Namespace] {
		if cache.Name == namespacedName.Name {
			len := len(cachedNames[namespacedName.Namespace])
			cachedNames[namespacedName.Namespace][i] = cachedNames[namespacedName.Namespace][len-1]
			cachedNames[namespacedName.Namespace][len-1] = nil
			cachedNames[namespacedName.Namespace] = cachedNames[namespacedName.Namespace][:len-1]
			log.V(3).Info("HVPA", namespacedName.Name, "removed from cache")
			break
		}
	}
}

// ManageCache manages the global map of HVPAs
func (r *HvpaReconciler) ManageCache(instance *autoscalingv1alpha1.Hvpa, namespacedName types.NamespacedName, foundHvpa bool) {
	cacheMux.Lock()
	defer cacheMux.Unlock()

	if !foundHvpa {
		// HVPA doesn't exist anymore, remove it from the cache
		removeFromCache(namespacedName)
		return
	}

	selector, err := r.getSelectorFromHvpa(instance)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", instance.Name)
		return
	}

	obj := hvpaObj{
		Name:     instance.Name,
		Selector: selector,
	}

	if _, ok := cachedNames[instance.Namespace]; !ok {
		if cachedNames == nil {
			cachedNames = make(map[string][]*hvpaObj)
		}
		cachedNames[instance.Namespace] = make([]*hvpaObj, 1)
		cachedNames[instance.Namespace] = []*hvpaObj{&obj}
	} else {
		found := false
		for _, cache := range cachedNames[instance.Namespace] {
			if cache.Name == obj.Name {
				found = true
				if !reflect.DeepEqual(cache.Selector, obj.Selector) {
					// Update selector if it has changed
					cache.Selector = obj.Selector
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

func (r *HvpaReconciler) reconcileVpa(hvpa *autoscalingv1alpha1.Hvpa) (*vpa_api.VerticalPodAutoscalerStatus, error) {
	selector, err := metav1.LabelSelectorAsSelector(hvpa.Spec.Vpa.Selector)
	if err != nil {
		log.Error(err, "Error converting vpa selector to selector")
		return nil, err
	}

	// list all vpas to include the vpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	vpas := &vpa_api.VerticalPodAutoscalerList{}
	err = r.List(context.TODO(), vpas, client.InNamespace(hvpa.Namespace))
	if err != nil {
		log.Error(err, "Error listing vpas")
		return nil, err
	}

	updatePolicy := hvpa.Spec.Vpa.UpdatePolicy.UpdateMode

	// NOTE: filteredVpas are pointing to deepcopies of the cache, but this could change in the future.
	// Ref: https://github.com/kubernetes-sigs/controller-runtime/blob/release-0.2/pkg/cache/internal/cache_reader.go#L74
	// if you need to modify them, you need to copy it first.
	filteredVpas, err := r.claimVpas(hvpa, selector, vpas)
	if err != nil {
		return nil, err
	}

	if len(filteredVpas) > 0 {
		// TODO: Sync spec and delete OR First delete and then sync spec?

		// VPAs are claimed by this HVPA. Just sync the specs
		if err = r.syncVpaSpec(filteredVpas, hvpa); err != nil {
			return nil, err
		}

		// Keep only 1 VPA. Delete the rest
		for i := 1; i < len(filteredVpas); i++ {
			vpa := filteredVpas[i]
			if err := r.Delete(context.TODO(), vpa); err != nil {
				log.Error(err, "Error in deleting duplicate VPAs")
				continue
			}
		}

		if *updatePolicy == autoscalingv1alpha1.UpdateModePurge {
			// If update policy is "Purge", then delete remaining VPA
			return nil, r.Delete(context.TODO(), filteredVpas[0])
		}

		// Return the updated VPA status
		vpa := &vpa_api.VerticalPodAutoscaler{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: filteredVpas[0].Name, Namespace: filteredVpas[0].Namespace}, vpa)
		return vpa.Status.DeepCopy(), err
	}

	// Required VPA doesn't exist. Create new

	if *updatePolicy == autoscalingv1alpha1.UpdateModePurge {
		// If update policy is "Purge", then return
		return nil, nil
	}

	vpa, err := getVpaFromHvpa(hvpa)
	if err != nil {
		return nil, err
	}

	if err := controllerutil.SetControllerReference(hvpa, vpa, r.Scheme); err != nil {
		return nil, err
	}

	if err := r.Create(context.TODO(), vpa); err != nil {
		return nil, err
	}

	return vpa.Status.DeepCopy(), nil
}

func (r *HvpaReconciler) reconcileHpa(hvpa *autoscalingv1alpha1.Hvpa) (*autoscaling.HorizontalPodAutoscalerStatus, error) {
	selector, err := metav1.LabelSelectorAsSelector(hvpa.Spec.Hpa.Selector)
	if err != nil {
		log.Error(err, "Error converting hpa selector to selector")
		return nil, err
	}

	// list all hpas to include the hpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	hpas := &autoscaling.HorizontalPodAutoscalerList{}
	err = r.List(context.TODO(), hpas, client.InNamespace(hvpa.Namespace))
	if err != nil {
		log.Error(err, "Error listing hpas")
		return nil, err
	}

	updatePolicy := hvpa.Spec.Hpa.UpdatePolicy.UpdateMode

	// NOTE: filteredHpas are pointing to deepcopies of the cache, but this could change in the future.
	// Ref: https://github.com/kubernetes-sigs/controller-runtime/blob/release-0.2/pkg/cache/internal/cache_reader.go#L74
	// if you need to modify them, you need to copy it first.
	filteredHpas, err := r.claimHpas(hvpa, selector, hpas)
	if err != nil {
		return nil, err
	}

	if len(filteredHpas) > 0 {
		// TODO: Sync spec and delete OR First delete and then sync spec?

		// HPAs are claimed by this HVPA. Just sync the specs
		if err = r.syncHpaSpec(filteredHpas, hvpa); err != nil {
			return nil, err
		}

		// Keep only 1 HPA. Delete the rest
		for i := 1; i < len(filteredHpas); i++ {
			hpa := filteredHpas[i]
			if err := r.Delete(context.TODO(), hpa); err != nil {
				log.Error(err, "Error in deleting duplicate HPAs")
				continue
			}
		}

		if *updatePolicy == autoscalingv1alpha1.UpdateModeOff || *updatePolicy == autoscalingv1alpha1.UpdateModePurge {
			// If update policy is "Off" or "Purge", then delete remaining HPA
			return nil, r.Delete(context.TODO(), filteredHpas[0])
		}

		// Return the updated HPA status
		hpa := &autoscaling.HorizontalPodAutoscaler{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: filteredHpas[0].Name, Namespace: filteredHpas[0].Namespace}, hpa)
		return hpa.Status.DeepCopy(), err
	}

	// Required HPA doesn't exist. Create new

	if *updatePolicy == autoscalingv1alpha1.UpdateModeOff || *updatePolicy == autoscalingv1alpha1.UpdateModePurge {
		// If update policy is "Off" or "Purge", then return
		return nil, nil
	}

	hpa, err := getHpaFromHvpa(hvpa)
	if err != nil {
		return nil, err
	}

	if err := controllerutil.SetControllerReference(hvpa, hpa, r.Scheme); err != nil {
		return nil, err
	}

	err = r.Create(context.TODO(), hpa)
	return hpa.Status.DeepCopy(), err
}

func getVpaWeightFromIntervals(hvpa *autoscalingv1alpha1.Hvpa, desiredReplicas, currentReplicas int32) autoscalingv1alpha1.VpaWeight {
	var vpaWeight autoscalingv1alpha1.VpaWeight
	// lastFraction is set to default 100 to handle the case when vpaWeight is 100 in the matching interval,
	// and there are no fractional vpaWeights in the previous intervals. So we need to default to this value
	lastFraction := autoscalingv1alpha1.VpaWeight(100)
	lookupNextFraction := false
	for _, interval := range hvpa.Spec.WeightBasedScalingIntervals {
		if lookupNextFraction {
			if interval.VpaWeight < 100 {
				vpaWeight = interval.VpaWeight
				break
			}
			continue
		}
		// TODO: Following 2 if checks need to be done as part of verification process
		if interval.StartReplicaCount == 0 {
			interval.StartReplicaCount = *hvpa.Spec.Hpa.Template.Spec.MinReplicas
		}
		if interval.LastReplicaCount == 0 {
			interval.LastReplicaCount = hvpa.Spec.Hpa.Template.Spec.MaxReplicas
		}
		if interval.VpaWeight < 100 {
			lastFraction = interval.VpaWeight
		}
		if currentReplicas >= interval.StartReplicaCount && currentReplicas <= interval.LastReplicaCount {
			vpaWeight = interval.VpaWeight
			if vpaWeight == 100 {
				if desiredReplicas < currentReplicas {
					// If HPA wants to scale in, use last seen fractional value as vpaWeight
					// If there is no such value, we cannot scale in anyway, so keep it default 100
					vpaWeight = lastFraction
				} else if desiredReplicas > currentReplicas {
					// If HPA wants to scale out, use next fractional value as vpaWeight
					// If there is no such value, we can not scale out anyway, so we will end up with vpaWeight = 100
					lookupNextFraction = true
					continue
				}
			}
			break
		}
	}
	return vpaWeight
}

func (r *HvpaReconciler) scaleIfRequired(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *autoscalingv1alpha1.Hvpa,
	target runtime.Object,
) (*autoscaling.HorizontalPodAutoscalerStatus,
	*vpa_api.VerticalPodAutoscalerStatus,
	bool, bool,
	autoscalingv1alpha1.VpaWeight,
	*[]*autoscalingv1alpha1.BlockedScaling,
	error) {

	var newObj runtime.Object
	var deploy *appsv1.Deployment
	var ss *appsv1.StatefulSet
	var ds *appsv1.DaemonSet
	var rs *appsv1.ReplicaSet
	var rc *corev1.ReplicationController
	var currentReplicas, weightedReplicas int32
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
		return nil, nil, false, false, 0, nil, err
	}

	var desiredReplicas int32
	if hpaStatus == nil {
		desiredReplicas = currentReplicas
	} else {
		desiredReplicas = hpaStatus.DesiredReplicas
	}

	vpaWeight := getVpaWeightFromIntervals(hvpa, desiredReplicas, currentReplicas)

	hpaScaleOutLimited := isHpaScaleOutLimited(hpaStatus, hvpa.Spec.Hpa.Template.Spec.MaxReplicas, hvpa.Spec.Hpa.UpdatePolicy.UpdateMode)

	blockedScaling := &[]*autoscalingv1alpha1.BlockedScaling{}

	// Memory for newPodSpec is assigned in the function getWeightedRequests
	newPodSpec, resourcesChanged, vpaStatus, err := getWeightedRequests(vpaStatus, hvpa, vpaWeight, podSpec, hpaScaleOutLimited, blockedScaling)
	if err != nil {
		log.Error(err, "Error in getting weight based requests in new deployment")
	}

	hpaStatus, err = getWeightedReplicas(hpaStatus, hvpa, currentReplicas, 100-vpaWeight, blockedScaling)
	if err != nil {
		log.Error(err, "Error in getting weight based replicas")
	}

	if hpaStatus == nil {
		weightedReplicas = currentReplicas
	} else {
		weightedReplicas = hpaStatus.DesiredReplicas
	}

	if currentReplicas == weightedReplicas &&
		(newPodSpec == nil || reflect.DeepEqual(podSpec, newPodSpec)) {
		log.V(3).Info("Scaling not required")
		return nil, nil, false, false, vpaWeight, blockedScaling, nil
	}

	hpaUpdateMode := *hvpa.Spec.Hpa.UpdatePolicy.UpdateMode
	vpaUpdateMode := *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode

	if hpaUpdateMode == autoscalingv1alpha1.UpdateModeOff && vpaUpdateMode == autoscalingv1alpha1.UpdateModeOff {
		log.V(4).Info("Update policy on HPA and VPA is set to Off")
		return hpaStatus, vpaStatus, false, false, vpaWeight, blockedScaling, nil
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
		return nil, nil, false, false, 0, nil, err
	}

	log.V(3).Info("Scaling required")
	return hpaStatus, vpaStatus,
		weightedReplicas != currentReplicas, resourcesChanged,
		vpaWeight, blockedScaling, r.Update(context.TODO(), newObj)
}

func getWeightedReplicas(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, currentReplicas int32, hpaWeight autoscalingv1alpha1.VpaWeight, blockedScaling *[]*autoscalingv1alpha1.BlockedScaling) (*autoscaling.HorizontalPodAutoscalerStatus, error) {
	anno := hvpa.GetAnnotations()
	if val, ok := anno["hpa-controller"]; !ok || val != "hvpa" {
		log.V(3).Info("HPA is not controlled by HVPA")
		return nil, nil
	}

	log.V(2).Info("Calculating weighted replicas", "hpaWeight", hpaWeight)
	if hpaStatus == nil || hpaStatus.DesiredReplicas == 0 {
		log.V(2).Info("HPA: Nothing to do")
		return nil, nil
	}

	var blockReason autoscalingv1alpha1.BlockingReason

	var err error
	var weightedReplicas int32
	desiredReplicas := hpaStatus.DesiredReplicas

	// Initialize output hpa status
	outHpaStatus := &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: currentReplicas,
	}

	if desiredReplicas == currentReplicas {
		log.V(2).Info("HPA", "no scaling required. Current replicas", currentReplicas)
		return outHpaStatus, err
	}

	if desiredReplicas > currentReplicas {
		weightedReplicas = int32(math.Ceil(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)/float64(100)))
	} else {
		weightedReplicas = int32(math.Floor(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)/float64(100)))
	}

	if weightedReplicas == currentReplicas {
		log.V(2).Info("HPA", "no scaling required. Weighted replicas", weightedReplicas)
		return outHpaStatus, err
	}

	lastScaleTime := hvpa.Status.LastScaling.LastScaleTime.DeepCopy()
	overrideScaleUpStabilization := hvpa.Status.OverrideScaleUpStabilization
	if overrideScaleUpStabilization {
		log.V(2).Info("HPA", "will override last scale time in case of scale out", overrideScaleUpStabilization)
	}
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := metav1.Now().Sub(lastScaleTime.Time)
	scaleUpStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleUpStabilization.Duration)
	scaleDownStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleDownStabilization.Duration)

	updateMode := *hvpa.Spec.Hpa.UpdatePolicy.UpdateMode

	if hpaWeight == 0 {
		blockReason = autoscalingv1alpha1.BlockingReasonWeight

	} else if updateMode == autoscalingv1alpha1.UpdateModePurge || updateMode == autoscalingv1alpha1.UpdateModeOff {
		blockReason = autoscalingv1alpha1.BlockingReasonUpdatePolicy

	} else if weightedReplicas > currentReplicas {
		if overrideScaleUpStabilization == false && lastScaleTimeDuration < scaleUpStabilizationWindow {
			blockReason = autoscalingv1alpha1.BlockingReasonStabilizationWindow
		} else {
			log.V(2).Info("HPA scaling up", "weighted replicas", weightedReplicas)
			outHpaStatus.DesiredReplicas = weightedReplicas
			return outHpaStatus, err
		}

	} else if weightedReplicas < currentReplicas {
		if updateMode == autoscalingv1alpha1.UpdateModeScaleUp {
			blockReason = autoscalingv1alpha1.BlockingReasonUpdatePolicy
		} else if overrideScaleUpStabilization == false && lastScaleTimeDuration < scaleDownStabilizationWindow {
			blockReason = autoscalingv1alpha1.BlockingReasonStabilizationWindow
		} else {
			log.V(2).Info("HPA scaling down", "weighted replicas", weightedReplicas)
			outHpaStatus.DesiredReplicas = weightedReplicas
			return outHpaStatus, err
		}
	}

	// Scaling is blocked for some reason if we are here.
	foundReason := false
	if blockedScaling != nil {
		for _, v := range *blockedScaling {
			if v != nil && v.Reason == blockReason {
				v.HpaStatus.DesiredReplicas = weightedReplicas
				v.HpaStatus.CurrentReplicas = currentReplicas
				foundReason = true
				break
			}
		}
	}

	if foundReason == false {
		blocked := newBlockedScaling(blockReason)
		blocked.HpaStatus.DesiredReplicas = weightedReplicas
		blocked.HpaStatus.CurrentReplicas = currentReplicas
		if blockedScaling == nil {
			err = fmt.Errorf("blockedScaling needs to be already populated")
			log.Error(err, "Error")
		} else {
			*blockedScaling = append(*blockedScaling, blocked)
		}
	}

	log.V(2).Info("HPA: scaling is blocked", "reason", blockReason, "currentReplicas", currentReplicas, "weightedReplicas", weightedReplicas, "minutes after last scaling", lastScaleTimeDuration.Minutes())
	return outHpaStatus, err
}

func isHpaScaleOutLimited(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, maxReplicas int32, hpaUpdateMode *string) bool {
	if *hpaUpdateMode == autoscalingv1alpha1.UpdateModeOff {
		// HPA update mode is off. So there can be no horizontal scaling
		return true
	}
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

func newBlockedScaling(reason autoscalingv1alpha1.BlockingReason) *autoscalingv1alpha1.BlockedScaling {
	blockedScaling := autoscalingv1alpha1.BlockedScaling{
		Reason: reason,
		ScalingStatus: autoscalingv1alpha1.ScalingStatus{
			VpaStatus: vpa_api.VerticalPodAutoscalerStatus{
				Recommendation: &vpa_api.RecommendedPodResources{
					ContainerRecommendations: make([]vpa_api.RecommendedContainerResources, 0, 0),
				},
			},
		},
	}
	return &blockedScaling
}

func appendToBlockedScaling(blockedScaling **autoscalingv1alpha1.BlockedScaling, reason autoscalingv1alpha1.BlockingReason, target corev1.ResourceList, container string, blocked bool) {
	if blocked {
		if *blockedScaling == nil {
			*blockedScaling = newBlockedScaling(reason)
		}
		(*blockedScaling).VpaStatus.Recommendation.ContainerRecommendations = append(
			(*blockedScaling).VpaStatus.Recommendation.ContainerRecommendations,
			vpa_api.RecommendedContainerResources{
				Target:        target,
				ContainerName: container,
			})
	}
}

// getWeightedRequests returns updated copy of podSpec if there is any change in podSpec,
// otherwise it returns nil
func getWeightedRequests(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, vpaWeight autoscalingv1alpha1.VpaWeight, podSpec *corev1.PodSpec, hpaScaleOutLimited bool, blockedScaling *[]*autoscalingv1alpha1.BlockedScaling) (*corev1.PodSpec, bool, *vpa_api.VerticalPodAutoscalerStatus, error) {
	log.V(2).Info("Checking if need to scale vertically")
	if vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.V(2).Info("VPA: Nothing to do")
		return nil, false, nil, nil
	}
	for k, v := range vpaStatus.Conditions {
		if v.Type == vpa_api.RecommendationProvided {
			if v.Status == "True" {
				// VPA recommendations are provided, we can do further processing
				break
			} else {
				log.V(2).Info("VPA recommendations not provided yet")
				return nil, false, nil, nil
			}
		}
		if k == len(vpaStatus.Conditions)-1 {
			log.V(2).Info("Reliable VPA recommendations not provided yet")
			return nil, false, nil, nil
		}
	}
	recommendations := vpaStatus.Recommendation

	lastScaleTime := hvpa.Status.LastScaling.LastScaleTime
	overrideScaleUpStabilization := hvpa.Status.OverrideScaleUpStabilization
	if overrideScaleUpStabilization {
		log.V(2).Info("VPA", "will override last scale time in case of scale up", overrideScaleUpStabilization)
		if vpaWeight == 0 {
			log.V(2).Info("VPA", "will override vpaWeight from 0 to 1")
			vpaWeight = 1
		}
	}
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := time.Now().Sub(lastScaleTime.Time)
	scaleUpStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleUpStabilization.Duration)
	scaleDownStabilizationWindow, _ := time.ParseDuration(*hvpa.Spec.ScaleDownStabilization.Duration)

	resourceChange := false

	newPodSpec := podSpec.DeepCopy()

	var blockedScalingWeight, blockedScalingUpdatePolicy, blockedScalingStabilizationWindow, blockedScalingMinChange *autoscalingv1alpha1.BlockedScaling

	len := len(vpaStatus.Recommendation.ContainerRecommendations)
	outVpaStatus := &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: make([]vpa_api.RecommendedContainerResources, 0, len),
		},
	}

	for _, rec := range recommendations.ContainerRecommendations {
		blockedByWeight := false
		blockedByUpdatePolicy := false
		blockedByStabilizationWindow := false
		blockedByMinChange := false

		outTarget := make(corev1.ResourceList)
		outTargetWeight := make(corev1.ResourceList)
		outTargetUpdatePolicy := make(corev1.ResourceList)
		outTargetStabilizationWindow := make(corev1.ResourceList)
		outTargetMinChanged := make(corev1.ResourceList)

		for id, container := range newPodSpec.Containers {
			if rec.ContainerName == container.Name {
				vpaMemTarget := rec.Target.Memory().DeepCopy()
				vpaCPUTarget := rec.Target.Cpu().DeepCopy()
				currMem := newPodSpec.Containers[id].Resources.Requests.Memory().DeepCopy()
				currCPU := newPodSpec.Containers[id].Resources.Requests.Cpu().DeepCopy()

				log.V(2).Info("VPA", "target mem", vpaMemTarget, "target cpu", vpaCPUTarget, "vpaWeight", vpaWeight, "minutes after last scaling", lastScaleTimeDuration.Minutes())

				factor := int64(100)
				scale := int64(vpaWeight)

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
				log.V(3).Info("VPA scale down", "minimum CPU delta", scaleDownMinDeltaCPU.String(), "minimum memory delta", scaleDownMinDeltaMem)
				log.V(3).Info("VPA scale up", "minimum CPU delta", scaleUpMinDeltaCPU.String(), "minimum memory delta", scaleUpMinDeltaMem, "HPA condition ScalingLimited", hpaScaleOutLimited)

				if vpaWeight == 0 {
					outTargetWeight[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
					blockedByWeight = true

				} else if diffMem.Sign() > 0 {
					if hpaScaleOutLimited == false || *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff {
						outTargetUpdatePolicy[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByUpdatePolicy = true

					} else if overrideScaleUpStabilization == false && lastScaleTimeDuration < scaleUpStabilizationWindow {
						outTargetStabilizationWindow[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByStabilizationWindow = true

					} else if diffMem.Cmp(*scaleUpMinDeltaMem) < 0 {
						outTargetMinChanged[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByMinChange = true

					} else {
						log.V(2).Info("VPA", "Scaling up", "memory", "Container", container.Name)
						newPodSpec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
						// Override VPA status in outVpaStatus with weighted value
						outTarget[corev1.ResourceMemory] = weightedMem.DeepCopy()
						resourceChange = true
					}
				} else if diffMem.Sign() < 0 {
					if *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff ||
						*hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeScaleUp {
						outTargetUpdatePolicy[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByUpdatePolicy = true

					} else if lastScaleTimeDuration < scaleDownStabilizationWindow {
						outTargetStabilizationWindow[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByStabilizationWindow = true

					} else if negDiffMem.Cmp(*scaleDownMinDeltaMem) < 0 {
						outTargetMinChanged[corev1.ResourceMemory] = rec.Target.Memory().DeepCopy()
						blockedByMinChange = true

					} else {
						log.V(2).Info("VPA", "Scaling down", "memory", "Container", container.Name)
						newPodSpec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
						// Override VPA status in outVpaStatus with weighted value
						outTarget[corev1.ResourceMemory] = weightedMem.DeepCopy()
						resourceChange = true
					}
				}

				if vpaWeight == 0 {
					outTargetWeight[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
					blockedByWeight = true

				} else if diffCPU.Sign() > 0 {
					if hpaScaleOutLimited == false || *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff {
						outTargetUpdatePolicy[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByUpdatePolicy = true

					} else if overrideScaleUpStabilization == false && lastScaleTimeDuration < scaleUpStabilizationWindow {
						outTargetStabilizationWindow[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByStabilizationWindow = true

					} else if diffCPU.Cmp(*scaleUpMinDeltaCPU) < 0 {
						outTargetMinChanged[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByMinChange = true

					} else {
						log.V(2).Info("VPA", "Scaling up", "CPU", "Container", container.Name)
						newPodSpec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
						// Override VPA status in outVpaStatus with weighted value
						outTarget[corev1.ResourceCPU] = weightedCPU.DeepCopy()
						resourceChange = true
					}
				} else if diffCPU.Sign() < 0 {
					if *hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeOff ||
						*hvpa.Spec.Vpa.UpdatePolicy.UpdateMode == autoscalingv1alpha1.UpdateModeScaleUp {
						outTargetUpdatePolicy[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByUpdatePolicy = true

					} else if lastScaleTimeDuration < scaleDownStabilizationWindow {
						outTargetStabilizationWindow[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByStabilizationWindow = true

					} else if negDiffCPU.Cmp(*scaleDownMinDeltaCPU) < 0 {
						outTargetMinChanged[corev1.ResourceCPU] = rec.Target.Cpu().DeepCopy()
						blockedByMinChange = true

					} else {
						log.V(2).Info("VPA", "Scaling down", "CPU", "Container", container.Name)
						newPodSpec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
						// Override VPA status in outVpaStatus with weighted value
						outTarget[corev1.ResourceCPU] = weightedCPU.DeepCopy()
						resourceChange = true
					}
				}

				// TODO: Add conditions for other resources also: ResourceStorage, ResourceEphemeralStorage,

				appendToBlockedScaling(&blockedScalingWeight, autoscalingv1alpha1.BlockingReasonWeight, outTargetWeight, container.Name, blockedByWeight)
				appendToBlockedScaling(&blockedScalingUpdatePolicy, autoscalingv1alpha1.BlockingReasonUpdatePolicy, outTargetUpdatePolicy, container.Name, blockedByUpdatePolicy)
				appendToBlockedScaling(&blockedScalingStabilizationWindow, autoscalingv1alpha1.BlockingReasonStabilizationWindow, outTargetStabilizationWindow, container.Name, blockedByStabilizationWindow)
				appendToBlockedScaling(&blockedScalingMinChange, autoscalingv1alpha1.BlockingReasonMinChange, outTargetMinChanged, container.Name, blockedByMinChange)

				outVpaStatus.Recommendation.ContainerRecommendations = append(outVpaStatus.Recommendation.ContainerRecommendations,
					vpa_api.RecommendedContainerResources{
						Target:        outTarget,
						ContainerName: container.Name,
					})
				break
			}
		}
	}
	if blockedScalingWeight != nil {
		*blockedScaling = append(*blockedScaling, blockedScalingWeight)
	}
	if blockedScalingUpdatePolicy != nil {
		*blockedScaling = append(*blockedScaling, blockedScalingUpdatePolicy)
	}
	if blockedScalingStabilizationWindow != nil {
		*blockedScaling = append(*blockedScaling, blockedScalingStabilizationWindow)
	}
	if blockedScalingMinChange != nil {
		*blockedScaling = append(*blockedScaling, blockedScalingMinChange)
	}

	log.V(2).Info("VPA", "vpa recommends changes?", resourceChange)
	if resourceChange {
		log.V(4).Info("VPA", "weighted recommendations", fmt.Sprintf("%+v", outVpaStatus.Recommendation.ContainerRecommendations))
		return newPodSpec, resourceChange, outVpaStatus, nil
	}
	return nil, false, nil, nil
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
	ctx := context.Background()

	// Fetch the Hvpa instance
	instance := &autoscalingv1alpha1.Hvpa{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			r.ManageCache(instance, req.NamespacedName, false)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	log.V(1).Info("Reconciling", "hvpa", instance.GetName())

	validationerr := validation.ValidateHvpa(instance)
	if validationerr.ToAggregate() != nil && len(validationerr.ToAggregate().Errors()) > 0 {
		log.Error(fmt.Errorf(validationerr.ToAggregate().Error()), "Validation of HVPA failed", "HVPA", instance.Name)
		return ctrl.Result{}, nil
	}

	r.ManageCache(instance, req.NamespacedName, true)

	// Default duration after which the object should be requeued
	requeAfter, _ := time.ParseDuration("1m")
	result := ctrl.Result{
		RequeueAfter: requeAfter,
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

	err = r.Get(ctx, types.NamespacedName{Name: instance.Spec.TargetRef.Name, Namespace: instance.Namespace}, obj)
	if err != nil {
		log.Error(err, "Error getting", "kind", instance.Spec.TargetRef.Kind, "name", instance.Spec.TargetRef.Name, "namespace", instance.Namespace)
		return ctrl.Result{}, err
	}

	if instance.GetDeletionTimestamp() != nil {
		log.V(2).Info("HVPA is under deletion. Skipping reconciliation", "HVPA", instance.Name)
		if r.deleteScalingMetrics(instance, obj) != nil {
			log.Error(err, "Error deleting scaling metrics")
		}
		return ctrl.Result{}, err
	}

	hpaStatus, err := r.reconcileHpa(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	vpaStatus, err := r.reconcileVpa(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	weightedHpaStatus, weightedVpaStatus, hpaScaled, vpaScaled, vpaWeight, blockedScaling, err := r.scaleIfRequired(hpaStatus, vpaStatus, instance, obj)
	if err != nil {
		return ctrl.Result{}, err
	}

	hvpa := instance.DeepCopy()
	if len(*blockedScaling) != 0 {
		hvpa.Status.LastBlockedScaling = *blockedScaling
	}

	// lookup cache for selector
	for _, obj := range cachedNames[hvpa.Namespace] {
		if obj.Name == hvpa.Name {
			selectorStr := obj.Selector.String()
			hvpa.Status.TargetSelector = &selectorStr
			break
		}
	}
	hvpa.Status.HpaUpdatePolicy = hvpa.Spec.Hpa.UpdatePolicy.DeepCopy()
	hvpa.Status.VpaUpdatePolicy = hvpa.Spec.Vpa.UpdatePolicy.DeepCopy()

	if hpaScaled || vpaScaled {
		now := metav1.Now()
		hvpa.Status.LastScaling.LastScaleTime = &now
		hvpa.Status.HpaWeight = 100 - vpaWeight
		hvpa.Status.VpaWeight = vpaWeight

		if hpaScaled {
			hvpa.Status.LastScaling.HpaStatus.DesiredReplicas = weightedHpaStatus.DesiredReplicas
			hvpa.Status.LastScaling.HpaStatus.CurrentReplicas = weightedHpaStatus.CurrentReplicas
		}

		if vpaScaled {
			hvpa.Status.LastScaling.VpaStatus.Recommendation = weightedVpaStatus.Recommendation.DeepCopy()
		}

		hvpa.Status.OverrideScaleUpStabilization = false
		/*// As only scale up is implemented yet
		if vpaScaled {
			hvpa.Status.HvpaStatus.LastScaleType.Vertical = autoscalingv1alpha1.Up
		}*/
	}

	if r.updateScalingMetrics(hvpa, hpaScaled, vpaScaled, obj) != nil {
		log.Error(err, "Error updating scaling metrics")
	}

	if !reflect.DeepEqual(hvpa.Status, instance.Status) {
		return result, r.Status().Update(ctx, hvpa)
	}

	return result, nil
}

func getPodEventHandler(mgr ctrl.Manager) *handler.EnqueueRequestsFromMapFunc {
	return &handler.EnqueueRequestsFromMapFunc{
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

			hvpaStatus := clone.Status
			if hvpaStatus.LastScaling.LastScaleTime == nil || hvpaStatus.OverrideScaleUpStabilization == true {
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
						clone.Status.LastScaling.LastScaleTime != nil &&
						containerStatus.LastTerminationState.Terminated.FinishedAt.After(clone.Status.LastScaling.LastScaleTime.Time) {

						recent = true
						break
					}
				}
				if !recent {
					log.V(4).Info("This is not  a recent oomkill. Return")
					return nil
				}
			}

			clone.Status.OverrideScaleUpStabilization = true

			log.V(2).Info("Updating HVPA status to override last scale time", "HVPA", clone.Name)
			err = client.Update(context.TODO(), clone)
			if err != nil {
				log.Error(err, "Error overrinding last scale time for", "HVPA", name)
			}

			return nil
		}),
	}
}

// SetupWithManager sets up manager with a new controller and r as the reconcile.Reconciler
func (r *HvpaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.AddMetrics(); err != nil {
		return err
	}

	podEventHandler := getPodEventHandler(mgr)

	pred := OomkillPredicate{
		Funcs: predicate.Funcs{
			UpdateFunc: updateEventFunc,
		},
	}

	podSource := source.Kind{Type: &corev1.Pod{}}
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.Hvpa{}).
		Owns(&autoscaling.HorizontalPodAutoscaler{}).
		Owns(&vpa_api.VerticalPodAutoscaler{}).
		Watches(&podSource, podEventHandler).
		WithEventFilter(pred).
		Complete(r)
}
