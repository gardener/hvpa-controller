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
	"math/big"
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
		log.V(3).Info("Handle update event for", "pod", newPod.Namespace+"/"+newPod.Name)
		return true
	}
	log.V(3).Info("Ignoring update event for", "pod", newPod.Namespace+"/"+newPod.Name)
	return false
}

func isEvictionEvent(oldPod, newPod *corev1.Pod) bool {
	if oldPod.Status.Reason == "Evicted" {
		log.V(4).Info("Pod was already evicted", oldPod.Namespace+"/"+oldPod.Name)
		return false
	}

	if newPod.Status.Reason == "Evicted" {
		log.V(4).Info("Pod was evicted", newPod.Namespace+"/"+newPod.Name)
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
			if oldStatus == nil || (oldStatus != nil && containerStatus.RestartCount > oldStatus.RestartCount) {
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
		log.Error(err, "Error getting target using targetRef.", "Will skip", instance.Namespace+"/"+instance.Name)
		return nil, err
	}

	selectorMap, found, err := unstructured.NestedMap(target.Object, "spec", "selector")
	if err != nil {
		log.Error(err, "Not able to get the selectorMap from target.", "Will skip", instance.Namespace+"/"+instance.Name)
		return nil, err
	}
	if !found {
		log.V(2).Info("Target doesn't have selector", "will skip HVPA", instance.Namespace+"/"+instance.Name)
		return nil, err
	}

	labelSelector := &metav1.LabelSelector{}
	selectorStr, err := json.Marshal(selectorMap)
	err = json.Unmarshal(selectorStr, &labelSelector)
	if err != nil {
		log.Error(err, "Error in reading selector string.", "will skip", instance.Namespace+"/"+instance.Name)
		return nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", instance.Namespace+"/"+instance.Name)
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
			log.V(3).Info("HVPA", namespacedName.String(), "removed from cache")
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
		log.Error(err, "Error in getting label selector", "will skip", instance.Namespace+"/"+instance.Name)
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
		log.Error(err, "Error converting vpa selector to selector", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, err
	}

	// list all vpas to include the vpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	vpas := &vpa_api.VerticalPodAutoscalerList{}
	err = r.List(context.TODO(), vpas, client.InNamespace(hvpa.Namespace))
	if err != nil {
		log.Error(err, "Error listing vpas", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, err
	}

	toDeploy := hvpa.Spec.Vpa.Deploy

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
				log.Error(err, "Error in deleting duplicate VPAs", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
				continue
			}
		}

		if toDeploy == false {
			// If VPA is not to be deployed, then delete remaining VPA
			return nil, r.Delete(context.TODO(), filteredVpas[0])
		}

		// Return the updated VPA status
		vpa := &vpa_api.VerticalPodAutoscaler{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: filteredVpas[0].Name, Namespace: filteredVpas[0].Namespace}, vpa)
		return vpa.Status.DeepCopy(), err
	}

	// Required VPA doesn't exist. Create new

	if toDeploy == false {
		// If VPA is not to be deployed, then return
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
		log.Error(err, "Error converting hpa selector to selector", "hvpa", hvpa.Namespace+"/"+hvpa.Namespace)
		return nil, err
	}

	// list all hpas to include the hpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	hpas := &autoscaling.HorizontalPodAutoscalerList{}
	err = r.List(context.TODO(), hpas, client.InNamespace(hvpa.Namespace))
	if err != nil {
		log.Error(err, "Error listing hpas", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, err
	}

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
				log.Error(err, "Error in deleting duplicate HPAs", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
				continue
			}
		}

		if hvpa.Spec.Hpa.Deploy == false {
			// delete remaining HPA
			return nil, r.Delete(context.TODO(), filteredHpas[0])
		}

		// Return the updated HPA status
		hpa := &autoscaling.HorizontalPodAutoscaler{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: filteredHpas[0].Name, Namespace: filteredHpas[0].Namespace}, hpa)
		return hpa.Status.DeepCopy(), err
	}

	// Required HPA doesn't exist. Create new

	if hvpa.Spec.Hpa.Deploy == false {
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

func areResourcesEqual(x, y *corev1.PodSpec) bool {
	for i := range x.Containers {
		containerX := &x.Containers[i]
		for j := range y.Containers {
			containerY := &y.Containers[j]
			if containerX.Name == containerY.Name {
				if containerX.Resources.Requests.Cpu().Cmp(*containerY.Resources.Requests.Cpu()) != 0 ||
					containerX.Resources.Requests.Memory().Cmp(*containerY.Resources.Requests.Memory()) != 0 {
					return false
				}
				break
			}
		}
	}
	return true
}

func (r *HvpaReconciler) scaleIfRequired(
	hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *autoscalingv1alpha1.Hvpa,
	target runtime.Object,
) (scaleStatus *autoscalingv1alpha1.ScalingStatus,
	hpaScaled, vpaScaled bool,
	blockedScalings *autoscalingv1alpha1.BlockedScaling,
	err error) {

	var newObj runtime.Object
	var deploy *appsv1.Deployment
	var ss *appsv1.StatefulSet
	var ds *appsv1.DaemonSet
	var rs *appsv1.ReplicaSet
	var rc *corev1.ReplicationController
	var currentReplicas, desiredReplicas int32
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
		err := fmt.Errorf("TargetRef kind not supported %v in hvpa %v", kind, hvpa.Namespace+"/"+hvpa.Name)
		log.Error(err, "Error")
		return nil, false, false, nil, err
	}

	scaledStatus, newPodSpec, resourcesChanged, blockingReason, err := getScalingRecommendations(hpaStatus, vpaStatus, hvpa, podSpec, currentReplicas)
	if err != nil {
		return nil, false, false, nil, err
	}
	if scaledStatus == nil {
		desiredReplicas = currentReplicas
	} else {
		desiredReplicas = scaledStatus.HpaStatus.DesiredReplicas
	}

	var blockedScaling *autoscalingv1alpha1.BlockedScaling
	if blockingReason != "" && hvpa.Status.OverrideScaleUpStabilization != true {
		blockedScaling = &autoscalingv1alpha1.BlockedScaling{Reason: blockingReason}
		blockedScaling.ScalingStatus = *scaledStatus
		log.V(3).Info("Scaling is blocked", "hvpa", hvpa.Namespace+"/"+hvpa.Name, "blocking reason", blockingReason)
		return nil, false, false, blockedScaling, nil
	}

	if currentReplicas == desiredReplicas &&
		(newPodSpec == nil || areResourcesEqual(podSpec, newPodSpec)) {
		log.V(3).Info("Scaling not required", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, false, false, blockedScaling, nil
	}

	switch kind {
	case "Deployment":
		deploy.Spec.Replicas = &desiredReplicas
		if newPodSpec != nil && resourcesChanged {
			newPodSpec.DeepCopyInto(&deploy.Spec.Template.Spec)
		}
		newObj = deploy
	case "StatefulSet":
		ss.Spec.Replicas = &desiredReplicas
		if newPodSpec != nil && resourcesChanged {
			newPodSpec.DeepCopyInto(&ss.Spec.Template.Spec)
		}
		newObj = ss
	case "DaemonSet":
		if newPodSpec != nil && resourcesChanged {
			newPodSpec.DeepCopyInto(&ds.Spec.Template.Spec)
		}
		newObj = ds
	case "ReplicaSet":
		rs.Spec.Replicas = &desiredReplicas
		if newPodSpec != nil && resourcesChanged {
			newPodSpec.DeepCopyInto(&rs.Spec.Template.Spec)
		}
		newObj = rs
	case "ReplicationController":
		rc.Spec.Replicas = &desiredReplicas
		if newPodSpec != nil && resourcesChanged {
			newPodSpec.DeepCopyInto(&rc.Spec.Template.Spec)
		}
		newObj = rc
	default:
		err := fmt.Errorf("TargetRef kind not supported %v", kind)
		log.Error(err, "Error")
		return nil, false, false, nil, err
	}

	log.V(3).Info("Scaling required", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
	return scaledStatus,
		desiredReplicas != currentReplicas, resourcesChanged, blockedScaling, r.Update(context.TODO(), newObj)
}

func initialzeIfRequired(resources *corev1.ResourceRequirements) {
	if resources.Requests == nil {
		resources.Requests = corev1.ResourceList{}
	}
	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{}
	}
}

func getScaledLimits(currLimits, currReq, newReq corev1.ResourceList, scaleParams autoscalingv1alpha1.ScaleParams) corev1.ResourceList {
	cpuLimit, msg := getScaledResourceLimit(corev1.ResourceCPU, currLimits.Cpu(), currReq.Cpu(), newReq.Cpu(), &scaleParams.CPU)
	if msg != "" {
		log.V(3).Info("VPA", "Warning", msg)
	}
	memLimit, msg := getScaledResourceLimit(corev1.ResourceMemory, currLimits.Memory(), currReq.Memory(), newReq.Memory(), &scaleParams.Memory)
	if msg != "" {
		log.V(3).Info("VPA", "Warning", msg)
	}

	result := corev1.ResourceList{}
	if cpuLimit != nil {
		_ = cpuLimit.String() // cache string q.s
		result[corev1.ResourceCPU] = *cpuLimit
	}
	if memLimit != nil {
		memLimit.SetScaled(memLimit.ScaledValue(resource.Kilo), resource.Kilo) // Set the scale to Kilo
		result[corev1.ResourceMemory] = *memLimit
	}
	return result
}

func getScaledResourceLimit(resourceName corev1.ResourceName, originalLimit, originalRequest, newRequest *resource.Quantity, changeParams *autoscalingv1alpha1.ChangeParams) (*resource.Quantity, string) {
	// originalLimit not set, don't set limit.
	if originalLimit == nil || originalLimit.Value() == 0 {
		return nil, ""
	}
	// originalLimit set but originalRequest not set - K8s will treat the pod as if they were equal,
	// recommend limit equal to request
	if originalRequest == nil || originalRequest.Value() == 0 {
		result := *newRequest
		return &result, ""
	}
	// originalLimit and originalRequest are set. If they are equal recommend limit equal to request.
	if originalRequest.MilliValue() == originalLimit.MilliValue() {
		result := *newRequest
		return &result, ""
	}
	// If change threshold is not provided, scale the limits proportinally
	if changeParams == nil ||
		(changeParams.Value == nil && (changeParams.Percentage == nil || *changeParams.Percentage == 0)) {
		result, capped := scaleQuantityProportionally( /*scaledQuantity=*/ originalLimit /*scaleBase=*/, originalRequest /*scaleResult=*/, newRequest)
		if !capped {
			return result, ""
		}
		return result, fmt.Sprintf(
			"%v: failed to keep limit to request ratio; capping limit to int64", resourceName)
	}

	// Initialise to max, and return the min after all the calculations
	var scaledPercentageVal, scaledAddedVal big.Int = *big.NewInt(math.MaxInt64), *big.NewInt(math.MaxInt64)
	newReqMilli := big.NewInt(newRequest.MilliValue())

	if changeParams.Percentage != nil && *changeParams.Percentage != 0 {
		scaledPercentageVal.Mul(newReqMilli, big.NewInt(int64(*changeParams.Percentage)))
		scaledPercentageVal.Div(&scaledPercentageVal, big.NewInt(100))
		scaledPercentageVal.Add(&scaledPercentageVal, newReqMilli)
	}

	if changeParams.Value != nil {
		delta := resource.MustParse(*changeParams.Value)
		deltaMilli := big.NewInt(delta.MilliValue())
		scaledAddedVal.Add(newReqMilli, deltaMilli)
	}

	if scaledAddedVal.Cmp(&scaledPercentageVal) == -1 {
		if scaledAddedVal.IsInt64() {
			return resource.NewMilliQuantity(scaledAddedVal.Int64(), originalLimit.Format), ""
		}
		return resource.NewMilliQuantity(math.MaxInt64, originalLimit.Format), fmt.Sprintf(
			"%v: failed to scale the limit as per limit scale parameters; capping limit to int64", resourceName)
	}

	if scaledPercentageVal.IsInt64() {
		return resource.NewMilliQuantity(scaledPercentageVal.Int64(), originalLimit.Format), ""
	}
	return resource.NewMilliQuantity(math.MaxInt64, originalLimit.Format), fmt.Sprintf(
		"%v: failed to scale the limit as per limit scale parameters; capping limit to int64", resourceName)
}

// scaleQuantityProportionally returns value which has the same proportion to scaledQuantity as scaleResult has to scaleBase
// It also returns a bool indicating if it had to cap result to MaxInt64 milliunits.
func scaleQuantityProportionally(scaledQuantity, scaleBase, scaleResult *resource.Quantity) (*resource.Quantity, bool) {
	originalMilli := big.NewInt(scaledQuantity.MilliValue())
	scaleBaseMilli := big.NewInt(scaleBase.MilliValue())
	scaleResultMilli := big.NewInt(scaleResult.MilliValue())
	var scaledOriginal big.Int
	scaledOriginal.Mul(originalMilli, scaleResultMilli)
	scaledOriginal.Div(&scaledOriginal, scaleBaseMilli)
	if scaledOriginal.IsInt64() {
		return resource.NewMilliQuantity(scaledOriginal.Int64(), scaledQuantity.Format), false
	}
	return resource.NewMilliQuantity(math.MaxInt64, scaledQuantity.Format), true
}

func getThreshold(thresholdVals *autoscalingv1alpha1.ChangeParams, resourceType corev1.ResourceName, currentVal resource.Quantity) int64 {
	const (
		defaultMemThreshold string = "200M"
		defaultCPUThreshold string = "200m"
	)
	var quantity resource.Quantity
	if thresholdVals == nil || (thresholdVals.Value == nil && thresholdVals.Percentage == nil) {
		if resourceType == corev1.ResourceMemory {
			// Set to default
			quantity = resource.MustParse(defaultMemThreshold)
		}
		if resourceType == corev1.ResourceCPU {
			// Set to default
			quantity = resource.MustParse(defaultCPUThreshold)
		}
		return quantity.MilliValue()
	}

	if thresholdVals.Percentage == nil && thresholdVals.Value != nil {
		quantity = resource.MustParse(*thresholdVals.Value)
		return quantity.MilliValue()
	}

	percentageValue := currentVal.ScaledValue(-3) * int64(*thresholdVals.Percentage) / 100
	percentageQuantity := resource.NewQuantity(percentageValue, currentVal.Format)
	percentageQuantity.SetScaled(percentageQuantity.Value(), -3)

	if thresholdVals.Value == nil {
		return percentageQuantity.MilliValue()
	}

	absoluteQuantity := resource.MustParse(*thresholdVals.Value)
	if percentageQuantity.Cmp(absoluteQuantity) < 0 {
		return percentageQuantity.MilliValue()
	}
	return absoluteQuantity.MilliValue()
}

func (r *HvpaReconciler) applyLastApplied(lastApplied *autoscalingv1alpha1.ScalingStatus, target runtime.Object, hvpa string) error {
	var newObj runtime.Object
	var deploy *appsv1.Deployment
	var ss *appsv1.StatefulSet
	var ds *appsv1.DaemonSet
	var rs *appsv1.ReplicaSet
	var rc *corev1.ReplicationController
	var replicas int32
	var podSpec *corev1.PodSpec
	var hpaScaled, vpaScaled bool

	kind := target.GetObjectKind().GroupVersionKind().Kind
	targetCopy := target.DeepCopyObject()

	switch kind {
	case "Deployment":
		deploy = targetCopy.(*appsv1.Deployment)
		replicas = *deploy.Spec.Replicas
		podSpec = &deploy.Spec.Template.Spec
	case "StatefulSet":
		ss = targetCopy.(*appsv1.StatefulSet)
		replicas = *ss.Spec.Replicas
		podSpec = &ss.Spec.Template.Spec
	case "DaemonSet":
		ds = targetCopy.(*appsv1.DaemonSet)
		podSpec = &ds.Spec.Template.Spec
	case "ReplicaSet":
		rs = targetCopy.(*appsv1.ReplicaSet)
		replicas = *rs.Spec.Replicas
		podSpec = &rs.Spec.Template.Spec
	case "ReplicationController":
		rc = targetCopy.(*corev1.ReplicationController)
		replicas = *rc.Spec.Replicas
		podSpec = &rc.Spec.Template.Spec
	default:
		err := fmt.Errorf("TargetRef kind not supported %v in hvpa %v", kind, hvpa)
		log.Error(err, "Error")
		return err
	}

	// If currentReplicas is 0, then don't change
	if replicas > 0 && lastApplied.HpaStatus.DesiredReplicas > 0 && replicas != lastApplied.HpaStatus.DesiredReplicas {
		hpaScaled = true
		replicas = lastApplied.HpaStatus.DesiredReplicas
	}

	newPodSpec := podSpec.DeepCopy()
	for i := range podSpec.Containers {
		container := &newPodSpec.Containers[i]
		for j := range lastApplied.VpaStatus.ContainerResources {
			applied := lastApplied.VpaStatus.ContainerResources[j]

			if container.Name == applied.ContainerName && !reflect.DeepEqual(container.Resources, applied.Resources) {
				vpaScaled = true
				container.Resources = applied.Resources
			}
		}
	}

	switch kind {
	case "Deployment":
		deploy.Spec.Replicas = &replicas
		if vpaScaled {
			newPodSpec.DeepCopyInto(&deploy.Spec.Template.Spec)
		}
		newObj = deploy
	case "StatefulSet":
		ss.Spec.Replicas = &replicas
		if vpaScaled {
			newPodSpec.DeepCopyInto(&ss.Spec.Template.Spec)
		}
		newObj = ss
	case "DaemonSet":
		if vpaScaled {
			newPodSpec.DeepCopyInto(&ds.Spec.Template.Spec)
		}
		newObj = ds
	case "ReplicaSet":
		rs.Spec.Replicas = &replicas
		if vpaScaled {
			newPodSpec.DeepCopyInto(&rs.Spec.Template.Spec)
		}
		newObj = rs
	case "ReplicationController":
		rc.Spec.Replicas = &replicas
		if vpaScaled {
			newPodSpec.DeepCopyInto(&rc.Spec.Template.Spec)
		}
		newObj = rc
	default:
		err := fmt.Errorf("TargetRef kind not supported %v", kind)
		log.Error(err, "Error")
		return err
	}

	if hpaScaled || vpaScaled {
		log.V(2).Info("Scaling required. Applying last applied", "hvpa", hvpa)
		return r.Update(context.TODO(), newObj)
	}
	log.V(3).Info("Scaling not required", "hvpa", hvpa)
	return nil
}

func getScalingStatusFrom(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, podSpec *corev1.PodSpec) *autoscalingv1alpha1.ScalingStatus {
	scalingStatus := &autoscalingv1alpha1.ScalingStatus{}

	if hpaStatus != nil {
		scalingStatus.HpaStatus = autoscalingv1alpha1.HpaStatus{
			CurrentReplicas: hpaStatus.CurrentReplicas,
			DesiredReplicas: hpaStatus.DesiredReplicas,
		}
	}

	if vpaStatus != nil && vpaStatus.Recommendation != nil {
		podResources := autoscalingv1alpha1.VpaStatus{
			ContainerResources: make([]autoscalingv1alpha1.ContainerResources, len(vpaStatus.Recommendation.ContainerRecommendations)),
		}
		for idx := range vpaStatus.Recommendation.ContainerRecommendations {
			recommendation := &vpaStatus.Recommendation.ContainerRecommendations[idx]
			podResources.ContainerResources[idx].ContainerName = recommendation.ContainerName
			podResources.ContainerResources[idx].Resources = corev1.ResourceRequirements{
				Requests: recommendation.Target,
			}
		}
		scalingStatus.VpaStatus = podResources
		if podSpec != nil {
			// If podSpec is passed, we want to capture "Resources" fully for containers for which VPA taget is available.
			// The podResources.ContainerResources[i] is populated here only for the container for which VPA target is available.
			for i := range scalingStatus.VpaStatus.ContainerResources {
				for j := range podSpec.Containers {
					container := podSpec.Containers[j]
					if scalingStatus.VpaStatus.ContainerResources[i].ContainerName == container.Name {
						scalingStatus.VpaStatus.ContainerResources[i].Resources = container.Resources
					}
				}
			}
		}
	}

	log.V(4).Info("HVPA", "scaling status:", scalingStatus)
	return scalingStatus
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

	// Fetch the Hvpa instance using unstructured object so that it is fetched using clientReader, and not the cacheReader
	hvpaObj := &unstructured.Unstructured{}
	hvpaObj.SetGroupVersionKind(controllerKindHvpa)

	err := r.Get(ctx, req.NamespacedName, hvpaObj)
	instance := &autoscalingv1alpha1.Hvpa{}
	if errInt := runtime.DefaultUnstructuredConverter.FromUnstructured(hvpaObj.Object, instance); errInt != nil {
		return ctrl.Result{}, errInt
	}
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

	log.V(1).Info("Reconciling", "hvpa", instance.GetName(), "hvpa", instance.Namespace+"/"+instance.Name)

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
		log.Error(err, "Error", "hvpa", instance.Namespace+"/"+instance.Name)
		// Don't return error, and requeue, so that reconciliation is tried after default sync period only
		return ctrl.Result{}, nil
	}

	err = r.Get(ctx, types.NamespacedName{Name: instance.Spec.TargetRef.Name, Namespace: instance.Namespace}, obj)
	if err != nil {
		log.Error(err, "Error getting", "kind", instance.Spec.TargetRef.Kind, "name", instance.Spec.TargetRef.Name, "namespace", instance.Namespace)
		return ctrl.Result{}, err
	}

	if instance.GetDeletionTimestamp() != nil {
		log.V(2).Info("HVPA is under deletion. Skipping reconciliation", "HVPA", instance.Namespace+"/"+instance.Name)
		if r.deleteScalingMetrics(instance, obj) != nil {
			log.Error(err, "Error deleting scaling metrics", "hvpa", instance.Namespace+"/"+instance.Name)
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

	processingStatus := getScalingStatusFrom(hpaStatus, vpaStatus, nil)
	// Prune currentReplicas, because it tends to change in hpa.Status if we end up scaling
	processingStatus.HpaStatus.CurrentReplicas = 0

	if !instance.Status.OverrideScaleUpStabilization && reflect.DeepEqual(*processingStatus, instance.Status.LastProcessedRecommendations) {
		log.V(3).Info("HVPA", "No new recommendations for", "hvpa", instance.Namespace+"/"+instance.Name)
		err = r.applyLastApplied(&instance.Status.LastScaling, obj, instance.Namespace+"/"+instance.Name)
		return result, nil
	}

	log.V(2).Info("HVPA", "Processing recommendations", processingStatus, "hvpa", instance.Namespace+"/"+instance.Name)

	scaledStatus, hpaScaled, vpaScaled, blockedScaling, err := r.scaleIfRequired(hpaStatus, vpaStatus, instance, obj)
	if err != nil {
		log.Error(err, "Error when scaling", "hvpa", instance.Namespace+"/"+instance.Name)
		return ctrl.Result{}, err
	}

	hvpa := instance.DeepCopy()
	now := metav1.Now()
	if blockedScaling != nil {
		blockedScaling.LastUpdated = &now
		hvpa.Status.LastBlockedScaling = blockedScaling
	}

	// lookup cache for selector
	for _, obj := range cachedNames[hvpa.Namespace] {
		if obj.Name == hvpa.Name {
			selectorStr := obj.Selector.String()
			hvpa.Status.TargetSelector = &selectorStr
			break
		}
	}

	hvpa.Status.ScaleUpUpdatePolicy = hvpa.Spec.ScaleUp.UpdatePolicy.DeepCopy()
	hvpa.Status.ScaleDownUpdatePolicy = hvpa.Spec.ScaleDown.UpdatePolicy.DeepCopy()

	if hpaScaled || vpaScaled {
		lastScaling := scaledStatus
		lastScaling.LastUpdated = &now

		// Prune
		if !hpaScaled {
			lastScaling.HpaStatus = autoscalingv1alpha1.HpaStatus{}
		}
		if !vpaScaled {
			lastScaling.VpaStatus = autoscalingv1alpha1.VpaStatus{}
		}

		hvpa.Status.LastScaling = *lastScaling

		hvpa.Status.LastProcessedRecommendations = *processingStatus

		hvpa.Status.OverrideScaleUpStabilization = false
	}

	if r.updateScalingMetrics(hvpa, hpaScaled, vpaScaled, obj) != nil {
		log.Error(err, "Error updating scaling metrics", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
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

			log.V(4).Info("Checking if need to override last scale time.", "hvpa", name, "pod", pod.Name, "namespace", pod.Namespace)
			// Get latest HVPA object
			hvpa := &autoscalingv1alpha1.Hvpa{}
			err := client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Meta.GetNamespace()}, hvpa)
			if err != nil {
				log.Error(err, "Error retreiving hvpa", "name", a.Meta.GetNamespace()+"/"+name)
				return nil
			}
			clone := hvpa.DeepCopy()

			hvpaStatus := clone.Status
			if hvpaStatus.OverrideScaleUpStabilization == true {
				log.V(4).Info("HVPA status already set to override last scale time.", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
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
					log.Error(err, "Error fetching node", "node", req.Name, "hvpa", hvpa.Namespace+"/"+hvpa.Name)
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
					log.V(4).Info("Pod was evicted, but the node is not under memory pressure.", "pod", pod.Namespace+"/"+pod.Name)
					return nil
				}
				log.V(4).Info("Pod might have been evited because node was under memory pressure", "pod", pod.Namespace+"/"+pod.Name)
			} else {
				// The pod was oomkilled
				// Build a map of containers controlled by HVPA
				vpaModeMap := make(map[string]vpa_api.ContainerScalingMode)
				for i := range clone.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies {
					containerPolicy := clone.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies[i]
					if containerPolicy.Mode != nil {
						vpaModeMap[containerPolicy.ContainerName] = *containerPolicy.Mode
					} else {
						vpaModeMap[containerPolicy.ContainerName] = vpa_api.ContainerScalingModeAuto
					}
				}
				// Process containers controlled by HVPA and check if scaling already happened after this oomkilled
				recent := false
				for i := range pod.Status.ContainerStatuses {
					containerStatus := &pod.Status.ContainerStatuses[i]
					if vpaModeMap[containerStatus.Name] != vpa_api.ContainerScalingModeOff &&
						containerStatus.RestartCount > 0 &&
						containerStatus.LastTerminationState.Terminated != nil &&
						containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" &&
						(clone.Status.LastScaling.LastUpdated == nil ||
							containerStatus.LastTerminationState.Terminated.FinishedAt.After(clone.Status.LastScaling.LastUpdated.Time)) {

						recent = true
						break
					}
				}
				if !recent {
					log.V(4).Info("This is not  a recent oomkill. Return", "pod", pod.Namespace+"/"+pod.Name)
					return nil
				}
			}

			clone.Status.OverrideScaleUpStabilization = true

			log.V(2).Info("Updating HVPA status to override last scale time", "HVPA", clone.Namespace+"/"+clone.Name)
			err = client.Status().Update(context.TODO(), clone)
			if err != nil {
				log.Error(err, "Error overrinding last scale time for", "HVPA", clone.Namespace+"/"+name)
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
