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

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	validation "github.com/gardener/hvpa-controller/internal/api/validation"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var controllerKindHvpa = hvpav1alpha1.SchemeGroupVersionHvpa.WithKind("Hvpa")

// HvpaReconciler reconciles a Hvpa object
type HvpaReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	EnableDetailedMetrics bool
	metrics               *hvpaMetrics
	cachedNames           map[string][]*hvpaObj
	cacheMux              sync.Mutex
}

var log = logf.Log.WithName("controller").WithName("hvpa")

func updateEventFunc(e event.UpdateEvent) bool {
	// If update event is for HVPA/HPA/VPA then we would want to reconcile unconditionally.
	switch t := e.ObjectOld.(type) {
	case *hvpav1alpha1.Hvpa, *autoscaling.HorizontalPodAutoscaler, *vpa_api.VerticalPodAutoscaler:
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

var ()

func (r *HvpaReconciler) getSelectorFromTargetObject(namespacedName types.NamespacedName, targetObj *unstructured.Unstructured) (labels.Selector, error) {
	selectorMap, found, err := unstructured.NestedMap(targetObj.Object, "spec", "selector")
	if err != nil {
		log.Error(err, "Not able to get the selectorMap from target.", "Will skip", namespacedName)
		return nil, err
	}
	if !found {
		log.V(2).Info("Target doesn't have selector", "will skip HVPA", namespacedName)
		return nil, err
	}

	labelSelector := &metav1.LabelSelector{}
	selectorStr, err := json.Marshal(selectorMap)
	err = json.Unmarshal(selectorStr, &labelSelector)
	if err != nil {
		log.Error(err, "Error in reading selector string.", "will skip", namespacedName)
		return nil, err
	}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", namespacedName)
		return nil, err
	}
	return selector, nil
}

// removeFromCache removes hvpa from the internal cache. The caller is responsible for synchronisation using cacheMux.
func (r *HvpaReconciler) removeFromCache(namespacedName types.NamespacedName) {
	for i, cache := range r.cachedNames[namespacedName.Namespace] {
		if cache.Name == namespacedName.Name {
			len := len(r.cachedNames[namespacedName.Namespace])
			r.cachedNames[namespacedName.Namespace][i] = r.cachedNames[namespacedName.Namespace][len-1]
			r.cachedNames[namespacedName.Namespace][len-1] = nil
			r.cachedNames[namespacedName.Namespace] = r.cachedNames[namespacedName.Namespace][:len-1]
			log.V(3).Info("HVPA", namespacedName.String(), "removed from cache")
			break
		}
	}
}

// ManageCache manages the global map of HVPAs
func (r *HvpaReconciler) ManageCache(namespacedName types.NamespacedName, targetObj *unstructured.Unstructured) error {
	r.cacheMux.Lock()
	defer r.cacheMux.Unlock()

	if targetObj == nil {
		// HVPA doesn't exist anymore, remove it from the cache
		r.removeFromCache(namespacedName)
		return nil
	}

	selector, err := r.getSelectorFromTargetObject(namespacedName, targetObj)
	if err != nil {
		log.Error(err, "Error in getting label selector", "will skip", namespacedName)
		fmt.Println(err)
		return err
	}

	obj := hvpaObj{
		Name:     namespacedName.Name,
		Selector: selector,
	}

	if _, ok := r.cachedNames[namespacedName.Namespace]; !ok {
		if r.cachedNames == nil {
			r.cachedNames = make(map[string][]*hvpaObj)
		}
		r.cachedNames[namespacedName.Namespace] = make([]*hvpaObj, 1)
		r.cachedNames[namespacedName.Namespace] = []*hvpaObj{&obj}
	} else {
		found := false
		for _, cache := range r.cachedNames[namespacedName.Namespace] {
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
			r.cachedNames[namespacedName.Namespace] = append(r.cachedNames[namespacedName.Namespace], &obj)
		}
	}
	log.V(4).Info("HVPA", "number of hvpas in cache", len(r.cachedNames[namespacedName.Namespace]), "for namespace", namespacedName.Namespace)
	return nil
}

func (r *HvpaReconciler) iterateOverCachedHVPAs(ns string, iterateFn func(*hvpaObj) (bool, error)) (bool, error) {
	r.cacheMux.Lock()
	defer r.cacheMux.Unlock()

	hvpas, ok := r.cachedNames[ns]
	if !ok {
		return false, nil
	}

	for _, obj := range hvpas {
		if done, err := iterateFn(obj); err != nil {
			return true, err
		} else if done {
			return true, nil
		}
	}

	return true, nil
}

func (r *HvpaReconciler) reconcileVpa(ctx context.Context, hvpa *hvpav1alpha1.Hvpa) (*vpa_api.VerticalPodAutoscalerStatus, error) {
	selector, err := metav1.LabelSelectorAsSelector(hvpa.Spec.Vpa.Selector)
	if err != nil {
		log.Error(err, "Error converting vpa selector to selector", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
		return nil, err
	}

	// list all vpas to include the vpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	vpas := &vpa_api.VerticalPodAutoscalerList{}
	err = r.List(ctx, vpas, client.InNamespace(hvpa.Namespace))
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
			if err := r.Delete(ctx, vpa); err != nil {
				log.Error(err, "Error in deleting duplicate VPAs", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
				continue
			}
		}

		if toDeploy == false {
			// If VPA is not to be deployed, then delete remaining VPA
			return nil, r.Delete(ctx, filteredVpas[0])
		}

		// Return the updated VPA status
		vpa := &vpa_api.VerticalPodAutoscaler{}
		err = r.Get(ctx, types.NamespacedName{Name: filteredVpas[0].Name, Namespace: filteredVpas[0].Namespace}, vpa)
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

	if err := r.Create(ctx, vpa); err != nil {
		return nil, err
	}

	return vpa.Status.DeepCopy(), nil
}

func (r *HvpaReconciler) reconcileHpa(ctx context.Context, hvpa *hvpav1alpha1.Hvpa) (*autoscaling.HorizontalPodAutoscalerStatus, error) {
	selector, err := metav1.LabelSelectorAsSelector(hvpa.Spec.Hpa.Selector)
	if err != nil {
		log.Error(err, "Error converting hpa selector to selector", "hvpa", hvpa.Namespace+"/"+hvpa.Namespace)
		return nil, err
	}

	// list all hpas to include the hpas that don't match the hvpa`s selector
	// anymore but has the stale controller ref.
	hpas := &autoscaling.HorizontalPodAutoscalerList{}
	err = r.List(ctx, hpas, client.InNamespace(hvpa.Namespace))
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
			if err := r.Delete(ctx, hpa); err != nil {
				log.Error(err, "Error in deleting duplicate HPAs", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
				continue
			}
		}

		if hvpa.Spec.Hpa.Deploy == false {
			// delete remaining HPA
			return nil, r.Delete(ctx, filteredHpas[0])
		}

		// Return the updated HPA status
		hpa := &autoscaling.HorizontalPodAutoscaler{}
		err = r.Get(ctx, types.NamespacedName{Name: filteredHpas[0].Name, Namespace: filteredHpas[0].Namespace}, hpa)
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

	err = r.Create(ctx, hpa)
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

func (r *HvpaReconciler) doScaleTarget(ctx context.Context, hvpa, target client.Object, recommenderFn func(currentReplicas int32, podSpec *corev1.PodSpec) (desiredReplicas int32, newPodSpec *corev1.PodSpec, err error)) (hpaScaled, vpaScaled bool, err error) {
	var (
		newObj                             client.Object
		deploy                             *appsv1.Deployment
		sts                                *appsv1.StatefulSet
		ds                                 *appsv1.DaemonSet
		rs                                 *appsv1.ReplicaSet
		rc                                 *corev1.ReplicationController
		currentReplicas, desiredReplicas   int32
		defaultReplicas                    = int32(1)
		podSpec, newPodSpec                *corev1.PodSpec
		scaleHorizontally, scaleVertically bool
	)

	gvk, err := apiutil.GVKForObject(target, r.Scheme)
	if err != nil {
		return false, false, err
	}

	switch gvk.Kind {
	case "Deployment":
		deploy = &appsv1.Deployment{}
		if err = r.Scheme.Convert(target, deploy, nil); err != nil {
			return false, false, err
		}
		currentReplicas = pointer.Int32Deref(deploy.Spec.Replicas, defaultReplicas)
		podSpec = &deploy.Spec.Template.Spec
	case "StatefulSet":
		sts = &appsv1.StatefulSet{}
		if err = r.Scheme.Convert(target, sts, nil); err != nil {
			return false, false, err
		}
		currentReplicas = pointer.Int32Deref(sts.Spec.Replicas, defaultReplicas)
		podSpec = &sts.Spec.Template.Spec
	case "DaemonSet":
		ds = &appsv1.DaemonSet{}
		if err = r.Scheme.Convert(target, ds, nil); err != nil {
			return false, false, err
		}
		podSpec = &ds.Spec.Template.Spec
	case "ReplicaSet":
		rs = &appsv1.ReplicaSet{}
		if err = r.Scheme.Convert(target, rs, nil); err != nil {
			return false, false, err
		}
		currentReplicas = pointer.Int32Deref(rs.Spec.Replicas, defaultReplicas)
		podSpec = &rs.Spec.Template.Spec
	case "ReplicationController":
		rc = &corev1.ReplicationController{}
		if err = r.Scheme.Convert(target, rc, nil); err != nil {
			return false, false, err
		}
		currentReplicas = pointer.Int32Deref(rc.Spec.Replicas, defaultReplicas)
		podSpec = &rc.Spec.Template.Spec
	default:
		err = fmt.Errorf("TargetRef kind not supported %v in hvpa %v", gvk.String(), client.ObjectKeyFromObject(hvpa))
		log.Error(err, "Error")
		return false, false, err
	}

	desiredReplicas, newPodSpec, err = recommenderFn(currentReplicas, podSpec.DeepCopy())

	if err != nil {
		return false, false, err
	}

	scaleHorizontally = currentReplicas != desiredReplicas
	scaleVertically = !reflect.DeepEqual(podSpec, newPodSpec)

	if !scaleHorizontally && !scaleVertically {
		log.V(3).Info("Scaling not required", "hvpa", client.ObjectKeyFromObject(hvpa))
		return false, false, nil
	}

	switch gvk.Kind {
	case "Deployment":
		if scaleHorizontally {
			deploy.Spec.Replicas = &desiredReplicas
		}
		if scaleVertically {
			newPodSpec.DeepCopyInto(&deploy.Spec.Template.Spec)
		}
		newObj = deploy
	case "StatefulSet":
		if scaleHorizontally {
			sts.Spec.Replicas = &desiredReplicas
		}
		if scaleVertically {
			newPodSpec.DeepCopyInto(&sts.Spec.Template.Spec)
		}
		newObj = sts
	case "DaemonSet":
		if scaleVertically {
			newPodSpec.DeepCopyInto(&ds.Spec.Template.Spec)
		}
		newObj = ds
	case "ReplicaSet":
		if scaleHorizontally {
			rs.Spec.Replicas = &desiredReplicas
		}
		if scaleVertically {
			newPodSpec.DeepCopyInto(&rs.Spec.Template.Spec)
		}
		newObj = rs
	case "ReplicationController":
		if scaleHorizontally {
			rc.Spec.Replicas = &desiredReplicas
		}
		if scaleVertically {
			newPodSpec.DeepCopyInto(&rc.Spec.Template.Spec)
		}
		newObj = rc
	default:
		err = fmt.Errorf("TargetRef kind not supported %v in hvpa %v", gvk.String(), client.ObjectKeyFromObject(hvpa))
		log.Error(err, "Error")
		return
	}

	log.V(3).Info("Scaling required", "hvpa", client.ObjectKeyFromObject(hvpa))
	return scaleHorizontally, scaleVertically, r.Update(ctx, newObj)
}

func (r *HvpaReconciler) scaleIfRequired(
	ctx context.Context,
	hpaStatus *autoscaling.HorizontalPodAutoscalerStatus,
	vpaStatus *vpa_api.VerticalPodAutoscalerStatus,
	hvpa *hvpav1alpha1.Hvpa,
	target client.Object,
) (scaledStatus *hvpav1alpha1.ScalingStatus,
	hpaScaled, vpaScaled bool,
	blockedScalings *[]*hvpav1alpha1.BlockedScaling,
	err error) {

	gvk, err := apiutil.GVKForObject(target, r.Scheme)
	if err != nil {
		return nil, false, false, nil, err
	}

	hpaScaled, vpaScaled, err = r.doScaleTarget(ctx, hvpa, target, func(currentReplicas int32, podSpec *corev1.PodSpec) (desiredReplicas int32, newPodSpec *corev1.PodSpec, err error) {
		if currentReplicas == 0 && gvk.Kind != "Daemonset" {
			log.V(3).Info("HVPA", "Not scaling because current replicas is 0", "hvpa", client.ObjectKeyFromObject(hvpa))
			return currentReplicas, podSpec, nil
		}

		scaledStatus, newPodSpec, _, blockedScalings, err = getScalingRecommendations(hpaStatus, vpaStatus, hvpa, podSpec, currentReplicas)

		if err != nil {
			return currentReplicas, podSpec, nil
		}

		if newPodSpec == nil {
			newPodSpec = podSpec.DeepCopy()
		}

		if scaledStatus != nil && scaledStatus.HpaStatus.DesiredReplicas > 0 { // Avoid scaling to zero based on recommendations
			desiredReplicas = scaledStatus.HpaStatus.DesiredReplicas
		} else {
			desiredReplicas = currentReplicas
		}

		return desiredReplicas, newPodSpec, nil
	})

	return scaledStatus, hpaScaled, vpaScaled, blockedScalings, err
}

func getScaledLimits(currLimits, currReq, newReq corev1.ResourceList, scaleParams hvpav1alpha1.ScaleParams) corev1.ResourceList {
	cpuLimit, msg := getScaledResourceLimit(corev1.ResourceCPU, currLimits.Cpu(), currReq.Cpu(), newReq.Cpu(), &scaleParams.CPU)
	if msg != "" {
		log.V(3).Info("VPA", "Warning", msg)
	}
	memLimit, msg := getScaledResourceLimit(corev1.ResourceMemory, currLimits.Memory(), currReq.Memory(), newReq.Memory(), &scaleParams.Memory)
	if msg != "" {
		log.V(3).Info("VPA", "Warning", msg)
	}

	if cpuLimit == nil && memLimit == nil {
		return nil
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

func getScaledResourceLimit(resourceName corev1.ResourceName, originalLimit, originalRequest, newRequest *resource.Quantity, changeParams *hvpav1alpha1.ChangeParams) (*resource.Quantity, string) {
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

	// Initialise to 0, and return the max after all the calculations
	var scaledPercentageVal, scaledAddedVal big.Int = *big.NewInt(0), *big.NewInt(0)
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

	if scaledAddedVal.Cmp(&scaledPercentageVal) == 1 {
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

func getThreshold(thresholdVals *hvpav1alpha1.ChangeParams, resourceType corev1.ResourceName, currentVal int64) int64 {
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

	percentageValue := currentVal * int64(*thresholdVals.Percentage) / 100

	if thresholdVals.Value == nil {
		return percentageValue
	}

	absoluteQuantity := resource.MustParse(*thresholdVals.Value)
	absValue := absoluteQuantity.MilliValue()
	if percentageValue < absValue {
		return percentageValue
	}
	return absValue
}

func (r *HvpaReconciler) applyLastApplied(ctx context.Context, lastApplied *hvpav1alpha1.ScalingStatus, hvpa, target client.Object) error {
	gvk, err := apiutil.GVKForObject(target, r.Scheme)
	if err != nil {
		return err
	}

	log.V(2).Info("Applying last applied", "hvpa", client.ObjectKeyFromObject(hvpa))
	_, _, err = r.doScaleTarget(ctx, hvpa, target, func(currentReplicas int32, podSpec *corev1.PodSpec) (desiredReplicas int32, newPodSpec *corev1.PodSpec, err error) {
		if currentReplicas == 0 && gvk.Kind != "Daemonset" {
			log.V(3).Info("HVPA", "Not scaling because current replicas is 0", "hvpa", client.ObjectKeyFromObject(hvpa))
			return currentReplicas, podSpec, nil

		}

		if lastApplied.HpaStatus.DesiredReplicas > 0 { // Avoid scaling to zero based on recommendations
			desiredReplicas = lastApplied.HpaStatus.DesiredReplicas
		} else {
			desiredReplicas = currentReplicas
		}

		newPodSpec = podSpec.DeepCopy()
		emptyResources := &corev1.ResourceRequirements{}
		for i := range podSpec.Containers {
			container := &newPodSpec.Containers[i]
			for j := range lastApplied.VpaStatus.ContainerResources {
				applied := lastApplied.VpaStatus.ContainerResources[j]

				if container.Name == applied.ContainerName {
					if reflect.DeepEqual(&applied.Resources, emptyResources) {
						continue // Avoid removing resources if nothing was applied earlier
					}
					applied.Resources.DeepCopyInto(&container.Resources)
				}
			}
		}

		return desiredReplicas, newPodSpec, nil
	})

	return err
}

func getScalingStatusFrom(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, podSpec *corev1.PodSpec) *hvpav1alpha1.ScalingStatus {
	scalingStatus := &hvpav1alpha1.ScalingStatus{}

	if hpaStatus != nil {
		scalingStatus.HpaStatus = hvpav1alpha1.HpaStatus{
			CurrentReplicas: hpaStatus.CurrentReplicas,
			DesiredReplicas: hpaStatus.DesiredReplicas,
		}
	}

	if vpaStatus != nil && vpaStatus.Recommendation != nil {
		podResources := hvpav1alpha1.VpaStatus{
			ContainerResources: make([]hvpav1alpha1.ContainerResources, len(vpaStatus.Recommendation.ContainerRecommendations)),
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
func (r *HvpaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance := &hvpav1alpha1.Hvpa{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, r.ManageCache(req.NamespacedName, nil)
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

	targetRef := instance.Spec.TargetRef
	targetObj := &unstructured.Unstructured{}
	targetObj.SetAPIVersion(targetRef.APIVersion)
	targetObj.SetKind(targetRef.Kind)

	if err = r.Get(ctx, types.NamespacedName{Name: targetRef.Name, Namespace: instance.Namespace}, targetObj); err != nil {
		log.Error(err, "Error getting", "kind", targetRef.Kind, "name", targetRef.Name, "namespace", instance.Namespace)
		return ctrl.Result{}, err
	}

	if instance.GetDeletionTimestamp() != nil {
		log.V(2).Info("HVPA is under deletion. Skipping reconciliation", "HVPA", instance.Namespace+"/"+instance.Name)
		var errs []error
		errs = append(errs, r.ManageCache(req.NamespacedName, nil))
		errs = append(errs, r.deleteScalingMetrics(instance, targetObj))
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	if err = r.ManageCache(req.NamespacedName, targetObj); err != nil {
		return ctrl.Result{}, err
	}

	// Default duration after which the object should be requeued
	requeAfter := 1 * time.Minute
	result := ctrl.Result{
		RequeueAfter: requeAfter,
	}

	hpaStatus, err := r.reconcileHpa(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	vpaStatus, err := r.reconcileVpa(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	processingStatus := getScalingStatusFrom(hpaStatus, vpaStatus, nil)
	// Prune currentReplicas, because it tends to change in hpa.Status if we end up scaling
	processingStatus.HpaStatus.CurrentReplicas = 0

	if !instance.Status.OverrideScaleUpStabilization && reflect.DeepEqual(*processingStatus, instance.Status.LastProcessedRecommendations) {
		log.V(3).Info("HVPA", "No new recommendations for", "hvpa", client.ObjectKeyFromObject(instance))
		if err = r.applyLastApplied(ctx, &instance.Status.LastScaling, instance, targetObj); err != nil {
			return result, err
		}

		return result, r.computeTargetSelectorAndUpdateStatus(ctx, instance.DeepCopy(), instance)
	}

	log.V(2).Info("HVPA", "Processing recommendations", processingStatus, "hvpa", client.ObjectKeyFromObject(instance))

	scaledStatus, hpaScaled, vpaScaled, blockedScaling, err := r.scaleIfRequired(ctx, hpaStatus, vpaStatus, instance, targetObj)
	if err != nil {
		log.Error(err, "Error when scaling", "hvpa", client.ObjectKeyFromObject(instance))
		return ctrl.Result{}, err
	}

	hvpa := instance.DeepCopy()
	now := metav1.Now()
	if blockedScaling != nil {
		hvpa.Status.LastBlockedScaling = *blockedScaling
	}

	hvpa.Status.ScaleUpUpdatePolicy = hvpa.Spec.ScaleUp.UpdatePolicy.DeepCopy()
	hvpa.Status.ScaleDownUpdatePolicy = hvpa.Spec.ScaleDown.UpdatePolicy.DeepCopy()

	if hpaScaled || vpaScaled {
		lastScaling := scaledStatus
		lastScaling.LastUpdated = &now

		// Prune
		if !hpaScaled {
			lastScaling.HpaStatus = hvpav1alpha1.HpaStatus{}
		}
		if !vpaScaled {
			lastScaling.VpaStatus = hvpav1alpha1.VpaStatus{}
		}

		hvpa.Status.LastScaling = *lastScaling

		hvpa.Status.LastProcessedRecommendations = *processingStatus

		hvpa.Status.OverrideScaleUpStabilization = false
	}

	if r.updateScalingMetrics(hvpa, hpaScaled, vpaScaled, targetObj) != nil {
		log.Error(err, "Error updating scaling metrics", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
	}

	return result, r.computeTargetSelectorAndUpdateStatus(ctx, hvpa, instance)
}

func (r *HvpaReconciler) computeTargetSelectorAndUpdateStatus(ctx context.Context, hvpa, original *hvpav1alpha1.Hvpa) error {
	// lookup cache for selector
	_, _ = r.iterateOverCachedHVPAs(hvpa.Namespace, func(obj *hvpaObj) (bool, error) {
		if obj.Name != hvpa.Name {
			return false, nil
		}

		selectorStr := obj.Selector.String()
		hvpa.Status.TargetSelector = &selectorStr
		return true, nil
	})

	if !reflect.DeepEqual(hvpa, original) {
		return r.Status().Update(ctx, hvpa)
	}

	return nil
}

func (r *HvpaReconciler) getPodEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(handler.MapFunc(func(a client.Object) []reconcile.Request {
		/* This event handler function, sets the flag on hvpa to override the last scale time stabilization window if:
		 * 1. The pod was oomkilled, OR
		 * 2. The pod was evicted, and the node was under memory pressure
		 */
		pod := a.(*corev1.Pod)
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			return nil
		}
		cl := r.Client

		// Get HVPA from the cache
		name := ""
		_, _ = r.iterateOverCachedHVPAs(pod.Namespace, func(obj *hvpaObj) (bool, error) {
			if !obj.Selector.Matches(labels.Set(pod.GetLabels())) {
				return false, nil
			}

			name = obj.Name
			return true, nil
		})

		if name == "" {
			// HVPA object not found for the pod
			return nil
		}

		ctx := context.TODO()

		log.V(4).Info("Checking if need to override last scale time.", "hvpa", name, "pod", pod.Name, "namespace", pod.Namespace)
		// Get latest HVPA object
		hvpa := &hvpav1alpha1.Hvpa{}
		err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: pod.Namespace}, hvpa)
		if err != nil {
			log.Error(err, "Error retreiving hvpa", "name", pod.Namespace+"/"+name)
			return nil
		}

		// Check if pod has latest resource values - we don't want to override stabilisation if target has different resources than this pod
		target := hvpa.Spec.TargetRef
		if target == nil {
			return nil
		}

		obj, err := scheme.Scheme.New(schema.FromAPIVersionAndKind(target.APIVersion, target.Kind))
		if err != nil {
			log.Error(err, "Error initializing runtime.Object for", "kind", target.Kind, "name", target.Name, "namespace", hvpa.Namespace)
			return nil
		}

		err = cl.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: hvpa.Namespace}, obj.(client.Object))
		if err != nil {
			log.Error(err, "Error getting", "kind", target.Kind, "name", target.Name, "namespace", hvpa.Namespace)
			return nil
		}

		var podTemplateSpec *corev1.PodSpec
		var currentReplicas int32
		switch target.Kind {
		case "Deployment":
			podTemplateSpec = &obj.(*appsv1.Deployment).Spec.Template.Spec
			currentReplicas = pointer.Int32Deref(obj.(*appsv1.Deployment).Spec.Replicas, int32(1))
		case "StatefulSet":
			podTemplateSpec = &obj.(*appsv1.StatefulSet).Spec.Template.Spec
			currentReplicas = pointer.Int32Deref(obj.(*appsv1.StatefulSet).Spec.Replicas, int32(1))
		case "DaemonSet":
			podTemplateSpec = &obj.(*appsv1.DaemonSet).Spec.Template.Spec
		case "ReplicaSet":
			podTemplateSpec = &obj.(*appsv1.ReplicaSet).Spec.Template.Spec
			currentReplicas = pointer.Int32Deref(obj.(*appsv1.ReplicaSet).Spec.Replicas, int32(1))
		case "ReplicationController":
			podTemplateSpec = &obj.(*corev1.ReplicationController).Spec.Template.Spec
			currentReplicas = pointer.Int32Deref(obj.(*corev1.ReplicationController).Spec.Replicas, int32(1))
		default:
			err := fmt.Errorf("TargetRef kind not supported %v in hvpa %v", hvpa.Spec.TargetRef.Kind, hvpa.Namespace+"/"+hvpa.Name)
			log.Error(err, "Error")
			return nil
		}

		if currentReplicas == 0 && target.Kind != "Daemonset" {
			log.V(3).Info("HVPA", "Not scaling because current replicas is 0", "hvpa", hvpa.Namespace+"/"+hvpa.Name)
			return nil
		}

		if !areResourcesEqual(&pod.Spec, podTemplateSpec) {
			log.V(3).Info("Ignoring pod event because the pod doesn't belong to latest replicaset", "pod", pod.Namespace+"/"+pod.Name)
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
			err := cl.Get(ctx, req, &node)
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
			if clone.Spec.Vpa.Template.Spec.ResourcePolicy != nil {
				for i := range clone.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies {
					containerPolicy := clone.Spec.Vpa.Template.Spec.ResourcePolicy.ContainerPolicies[i]
					if containerPolicy.Mode != nil {
						vpaModeMap[containerPolicy.ContainerName] = *containerPolicy.Mode
					} else {
						vpaModeMap[containerPolicy.ContainerName] = vpa_api.ContainerScalingModeAuto
					}
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
		err = cl.Status().Update(ctx, clone)
		if err != nil {
			log.Error(err, "Error overrinding last scale time for", "HVPA", clone.Namespace+"/"+name)
		}

		return nil
	}))
}

// SetupWithManager sets up manager with a new controller and r as the reconcile.Reconciler
func (r *HvpaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.AddMetrics(true); err != nil {
		return err
	}

	podEventHandler := r.getPodEventHandler()

	pred := OomkillPredicate{
		Funcs: predicate.Funcs{
			UpdateFunc: updateEventFunc,
		},
	}

	podSource := source.Kind{Type: &corev1.Pod{}}
	return ctrl.NewControllerManagedBy(mgr).
		For(&hvpav1alpha1.Hvpa{}).
		Owns(&autoscaling.HorizontalPodAutoscaler{}).
		Owns(&vpa_api.VerticalPodAutoscaler{}).
		Watches(&podSource, podEventHandler).
		WithEventFilter(pred).
		Complete(r)
}
