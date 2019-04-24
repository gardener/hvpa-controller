/*
Copyright 2019 Gaurav Gupta.

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

package hvpa

import (
	"context"
	"math"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	autoscalingv1alpha1 "k8s.io/autoscaler/hvpa-controller/pkg/apis/autoscaling/v1alpha1"

	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller")

// Add creates a new Hvpa Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileHvpa{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("hvpa-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to Hvpa
	err = c.Watch(&source.Kind{Type: &autoscalingv1alpha1.Hvpa{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// watch a HPA and VPA created by Hvpa
	err = c.Watch(&source.Kind{Type: &autoscaling.HorizontalPodAutoscaler{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &autoscalingv1alpha1.Hvpa{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &vpa_api.VerticalPodAutoscaler{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &autoscalingv1alpha1.Hvpa{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileHvpa{}

// ReconcileHvpa reconciles a Hvpa object
type ReconcileHvpa struct {
	client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Hvpa object and makes changes based on the state read
// and what is in the Hvpa.Spec
// Automatically generate RBAC rules to allow the Controller to read and write HPAs and VPAs
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=hvpas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=hvpas/status,verbs=get;update;patch
func (r *ReconcileHvpa) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the Hvpa instance
	instance := &autoscalingv1alpha1.Hvpa{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	log.Info("Reconciling", "hvpa", instance.GetName())

	hpaStatus, err := r.reconcileHpa(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	vpaStatus, err := r.reconcileVpa(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.TargetRef.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		log.Info("Error getting", "kind", instance.Spec.TargetRef.Kind, "name", instance.Spec.TargetRef.Name, "namespace", instance.Namespace)
		return reconcile.Result{}, err
	}

	hpaScaled, vpaScaled, vpaWeight, err := r.scaleIfRequired(hpaStatus, vpaStatus, instance, deploy)
	if err != nil {
		return reconcile.Result{}, err
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
		now := metav1.NewTime(time.Now())
		hvpa.Status.HvpaStatus.LastScaleTime = &now
		hvpa.Status.HvpaStatus.HpaWeight = 1 - vpaWeight
		hvpa.Status.HvpaStatus.VpaWeight = vpaWeight
		if hpaScaled > 0 {
			hvpa.Status.HvpaStatus.LastScaleType.Horizontal = autoscalingv1alpha1.Out
		} else if hpaScaled < 0 {
			hvpa.Status.HvpaStatus.LastScaleType.Horizontal = autoscalingv1alpha1.In
		}
		/*// As only scale up is implemented yet
		if vpaScaled {
			hvpa.Status.HvpaStatus.LastScaleType.Vertical = autoscalingv1alpha1.Up
		}*/
	}
	if !reflect.DeepEqual(hvpa.Status, instance.Status) {
		return reconcile.Result{}, r.Update(context.TODO(), hvpa)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileHvpa) reconcileVpa(hvpa *autoscalingv1alpha1.Hvpa) (*vpa_api.VerticalPodAutoscalerStatus, error) {
	vpa := &vpa_api.VerticalPodAutoscaler{
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
		},
	}

	if err := controllerutil.SetControllerReference(hvpa, vpa, r.scheme); err != nil {
		return nil, err
	}

	foundVpa := &vpa_api.VerticalPodAutoscaler{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: vpa.Name, Namespace: vpa.Namespace}, foundVpa)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating VPA", "namespace", vpa.Namespace, "name", vpa.Name)
		err = r.Create(context.TODO(), vpa)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	// Update the found object and write the result back if there are any changes
	if !reflect.DeepEqual(vpa.Spec, foundVpa.Spec) {
		foundVpa.Spec = vpa.Spec
		log.Info("Updating VPA", "namespace", vpa.Namespace, "name", vpa.Name)
		err = r.Update(context.TODO(), foundVpa)
		if err != nil {
			return nil, err
		}
	}

	status := foundVpa.Status.DeepCopy()

	return status, nil
}
func (r *ReconcileHvpa) reconcileHpa(hvpa *autoscalingv1alpha1.Hvpa) (*autoscaling.HorizontalPodAutoscalerStatus, error) {
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
	if val, ok := anno["hpa-controller"]; !ok || val != "kcm" {
		// If this annotation is not set on hvpa, OR
		// If the value of this annotation on hvpa is not kcm, then set hpa's mode off
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
		log.Info("Creating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
		err = r.Create(context.TODO(), hpa)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	// Update the found object and write the result back if there are any changes
	if !reflect.DeepEqual(hpa.Spec, foundHpa.Spec) || !reflect.DeepEqual(hpa.GetAnnotations, foundHpa.GetAnnotations) {
		foundHpa.Spec = hpa.Spec
		foundHpa.SetAnnotations(hpa.GetAnnotations())
		log.Info("Updating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
		err = r.Update(context.TODO(), foundHpa)
		if err != nil {
			return nil, err
		}
	}

	status := foundHpa.Status.DeepCopy()
	return status, nil
}

func (r *ReconcileHvpa) hpaScaleIfRequired(desiredReplicas, currentReplicas int32, hvpa *autoscalingv1alpha1.Hvpa, deployment *appsv1.Deployment, hpaWeight autoscalingv1alpha1.VpaWeight) (int32, error) {
	if desiredReplicas == 0 {
		return 0, nil
	}
	log.Info("Checking if need to scale horizontally")

	var err error
	minReplicas := *hvpa.Spec.HpaTemplate.MinReplicas
	maxReplicas := hvpa.Spec.HpaTemplate.MaxReplicas

	weightedReplicas := int32(math.Ceil(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)))
	if weightedReplicas < minReplicas {
		weightedReplicas = minReplicas
	}
	if weightedReplicas > maxReplicas {
		weightedReplicas = maxReplicas
	}

	if currentReplicas != weightedReplicas {
		if hvpa.Status.HvpaStatus.LastScaleTime != nil {
			var threshold time.Duration
			timeLapsed := time.Now().Sub(hvpa.Status.HvpaStatus.LastScaleTime.Time)
			if weightedReplicas > currentReplicas && hvpa.Status.HvpaStatus.LastScaleType.Horizontal == autoscalingv1alpha1.Out {
				threshold, err = time.ParseDuration(hvpa.Spec.ScaleUpDelay)
				if err != nil {
					log.Error(err, "Error in parsing duration", "provided string", hvpa.Spec.ScaleUpDelay)
					return 0, err
				}
			}
			if weightedReplicas < currentReplicas && hvpa.Status.HvpaStatus.LastScaleType.Horizontal == autoscalingv1alpha1.In {
				threshold, err = time.ParseDuration(hvpa.Spec.ScaleDownDelay)
				if err != nil {
					log.Error(err, "Error in parsing duration", "provided string", hvpa.Spec.ScaleDownDelay)
					return 0, err
				}
			}
			if timeLapsed < threshold {
				log.Info("Not scaling out as it was done recently")
				return 0, nil
			}
		}
		log.Info("Scaling horizontally", "current Replicas", currentReplicas, "weighted desired Replicas", weightedReplicas)

		newDeploy := deployment.DeepCopy()
		if *newDeploy.Spec.Replicas != weightedReplicas {
			replicas := *newDeploy.Spec.Replicas
			newDeploy.Spec.Replicas = &weightedReplicas

			log.Info("HPA", "Scaled horizontally to", weightedReplicas)
			return weightedReplicas - replicas, r.Update(context.TODO(), newDeploy)
		}
	}
	log.Info("No horizontal scaling done")
	return 0, nil
}

func (r *ReconcileHvpa) vpaScaleIfRequired(recommendations *vpa_api.RecommendedPodResources, hvpa *autoscalingv1alpha1.Hvpa, deployment *appsv1.Deployment, vpaWeight autoscalingv1alpha1.VpaWeight) (bool, error) {
	log.Info("Checking if need to scale vertically")
	if recommendations == nil {
		log.Info("No recommendations yet")
		return false, nil
	}
	resourceChange := false
	newDeploy := deployment.DeepCopy()
	for _, rec := range recommendations.ContainerRecommendations {
		for id, container := range newDeploy.Spec.Template.Spec.Containers {
			// Currently only scale up is implemented, and vpaWeight is assumed to be 1
			if rec.ContainerName == container.Name {
				vpaMemTarget := rec.Target.Memory().DeepCopy()
				vpaCPUTarget := rec.Target.Cpu().DeepCopy()
				currMem := newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests.Memory().DeepCopy()
				currCPU := newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests.Cpu().DeepCopy()

				log.Info("VPA", "target mem", vpaMemTarget, "target cpu", vpaCPUTarget, "vpaWeight", vpaWeight)

				factor := int64(100)
				scale := int64(float64(vpaWeight) * float64(factor))

				//origMem := currMem.DeepCopy()
				vpaMemTarget.Sub(currMem)
				diffMem := resource.NewQuantity(vpaMemTarget.Value()*scale/factor, vpaMemTarget.Format)
				currMem.Add(*diffMem)
				weightedMem := currMem
				minDeltaMem, _ := resource.ParseQuantity("500M")

				//origCPU := currCPU.DeepCopy()
				vpaCPUTarget.Sub(currCPU)
				diffCPU := resource.NewQuantity(vpaCPUTarget.ScaledValue(-3)*scale/factor, vpaCPUTarget.Format)
				diffCPU.SetScaled(diffCPU.Value(), -3)
				currCPU.Add(*diffCPU)
				weightedCPU := currCPU
				minDeltaCPU, _ := resource.ParseQuantity("300m")

				log.Info("VPA", "weighted target mem", weightedMem, "weighted target cpu", weightedCPU)
				//if weightedMem.Cmp(origMem) > 0 {
				if diffMem.Sign() > 0 && diffMem.Cmp(minDeltaMem) > 0 {
					// If the difference is greater than minimum delta
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
					resourceChange = true
				}
				//if weightedCPU.Cmp(origCPU) > 0 {
				if diffCPU.Sign() > 0 && diffCPU.Cmp(minDeltaCPU) > 0 {
					// If the difference is greater than minimum delta
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
					resourceChange = true
				}
				// TODO: Add conditions for other resources also: ResourceStorage, ResourceEphemeralStorage,
				break
			}
		}
	}
	log.Info("VPA", "vpa made changes?", resourceChange)
	if resourceChange {
		return true, r.Update(context.TODO(), newDeploy)
	}
	return false, nil
}

func (r *ReconcileHvpa) scaleIfRequired(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, deployment *appsv1.Deployment) (int32, bool, autoscalingv1alpha1.VpaWeight, error) {

	currentReplicas := *deployment.Spec.Replicas
	var desiredReplicas int32
	if hpaStatus == nil {
		desiredReplicas = currentReplicas
	} else {
		desiredReplicas = hpaStatus.DesiredReplicas
	}

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

	// Memory for newDeploy is assigned in the function getWeightedRequests
	newDeploy, resourceChanged, err := getWeightedRequests(vpaStatus, hvpa, vpaWeight, deployment)
	if err != nil {
		log.Error(err, "Error in getting weight based requests in new deployment")
	}

	weightedReplicas, err := getWeightedReplicas(hpaStatus, hvpa, deployment, 1-vpaWeight)
	if err != nil {
		log.Error(err, "Error in getting weight based replicas")
	}

	if weightedReplicas == 0 {
		weightedReplicas = currentReplicas
	}

	if currentReplicas != weightedReplicas {
		if newDeploy == nil {
			newDeploy = deployment.DeepCopy()
		}
		newDeploy.Spec.Replicas = &weightedReplicas

		log.Info("HPA", "Scale horizontally from", currentReplicas, "to", weightedReplicas)
	}
	if newDeploy == nil {
		log.Info("Scaling not required")
		return 0, false, vpaWeight, nil
	}
	log.Info("Scaling required")
	return weightedReplicas - currentReplicas, resourceChanged, vpaWeight, r.Update(context.TODO(), newDeploy)
}

func getWeightedReplicas(hpaStatus *autoscaling.HorizontalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, deployment *appsv1.Deployment, hpaWeight autoscalingv1alpha1.VpaWeight) (int32, error) {
	anno := hvpa.GetAnnotations()
	if val, ok := anno["hpa-controller"]; ok {
		if val == "kcm" {
			// HPA is controlled by kube-controller-manager
			log.Info("HVPA controller is not controlling HPA")
			return 0, nil
		}
	}

	log.Info("Calculating weighted replicas")
	if hpaWeight == 0 || hpaStatus == nil || hpaStatus.DesiredReplicas == 0 {
		log.Info("Nothing to do")
		return 0, nil
	}

	lastScaleTime := hvpa.Status.HvpaStatus.LastScaleTime
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := time.Now().Sub(lastScaleTime.Time)
	scaleUpDelay, _ := time.ParseDuration(hvpa.Spec.ScaleUpDelay)
	scaleDownDelay, _ := time.ParseDuration(hvpa.Spec.ScaleDownDelay)

	var err error
	minReplicas := *hvpa.Spec.HpaTemplate.MinReplicas
	maxReplicas := hvpa.Spec.HpaTemplate.MaxReplicas
	currentReplicas := *deployment.Spec.Replicas
	desiredReplicas := hpaStatus.DesiredReplicas

	weightedReplicas := int32(math.Ceil(float64(currentReplicas) + float64(desiredReplicas-currentReplicas)*float64(hpaWeight)))
	if weightedReplicas < minReplicas {
		weightedReplicas = minReplicas
	}
	if weightedReplicas > maxReplicas {
		weightedReplicas = maxReplicas
	}

	if (weightedReplicas > currentReplicas && lastScaleTimeDuration > scaleUpDelay) ||
		(weightedReplicas < currentReplicas && lastScaleTimeDuration > scaleDownDelay) {
		log.Info("HPA", "weighted replicas", weightedReplicas)
		return weightedReplicas, err
	}

	log.Info("HPA", "no scaling done. Current replicas", currentReplicas)
	return currentReplicas, err
}

func getWeightedRequests(vpaStatus *vpa_api.VerticalPodAutoscalerStatus, hvpa *autoscalingv1alpha1.Hvpa, vpaWeight autoscalingv1alpha1.VpaWeight, deployment *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
	log.Info("Checking if need to scale vertically")
	if vpaWeight == 0 || vpaStatus == nil || vpaStatus.Recommendation == nil {
		log.Info("Nothing to do")
		return nil, false, nil
	}
	for k, v := range vpaStatus.Conditions {
		if v.Type == vpa_api.RecommendationProvided {
			if v.Status == "True" {
				// VPA recommendations are provided, we can do further processing
				break
			} else {
				log.Info("VPA recommendations not provided yet")
				return nil, false, nil
			}
		}
		if k == len(vpaStatus.Conditions)-1 {
			log.Info("Reliable VPA recommendations not provided yet")
			return nil, false, nil
		}
	}
	recommendations := vpaStatus.Recommendation

	lastScaleTime := hvpa.Status.HvpaStatus.LastScaleTime
	if lastScaleTime == nil {
		lastScaleTime = &metav1.Time{}
	}
	lastScaleTimeDuration := time.Now().Sub(lastScaleTime.Time)
	scaleUpDelay, _ := time.ParseDuration(hvpa.Spec.ScaleUpDelay)
	scaleDownDelay, _ := time.ParseDuration(hvpa.Spec.ScaleDownDelay)

	resourceChange := false
	newDeploy := deployment.DeepCopy()
	for _, rec := range recommendations.ContainerRecommendations {
		for id, container := range newDeploy.Spec.Template.Spec.Containers {
			// Currently only scale up is implemented, and vpaWeight is assumed to be 1
			if rec.ContainerName == container.Name {
				vpaMemTarget := rec.Target.Memory().DeepCopy()
				vpaCPUTarget := rec.Target.Cpu().DeepCopy()
				currMem := newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests.Memory().DeepCopy()
				currCPU := newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests.Cpu().DeepCopy()

				log.Info("VPA", "target mem", vpaMemTarget, "target cpu", vpaCPUTarget, "vpaWeight", vpaWeight, "minutes after last scaling", lastScaleTimeDuration.Minutes())

				factor := int64(100)
				scale := int64(float64(vpaWeight) * float64(factor))

				minDeltaMem, _ := getThreshold(hvpa.Spec.MinMemChange, corev1.ResourceMemory, currMem)
				vpaMemTarget.Sub(currMem)
				diffMem := resource.NewQuantity(vpaMemTarget.Value()*scale/factor, vpaMemTarget.Format)
				negDiffMem := resource.NewQuantity(-vpaMemTarget.Value()*scale/factor, vpaMemTarget.Format)
				currMem.Add(*diffMem)
				weightedMem := currMem

				minDeltaCPU, _ := getThreshold(hvpa.Spec.MinCPUChange, corev1.ResourceCPU, currCPU)
				vpaCPUTarget.Sub(currCPU)
				diffCPU := resource.NewQuantity(vpaCPUTarget.ScaledValue(-3)*scale/factor, vpaCPUTarget.Format)
				negDiffCPU := resource.NewQuantity(-vpaCPUTarget.ScaledValue(-3)*scale/factor, vpaCPUTarget.Format)
				diffCPU.SetScaled(diffCPU.Value(), -3)
				currCPU.Add(*diffCPU)
				weightedCPU := currCPU

				log.Info("VPA", "weighted target mem", weightedMem, "weighted target cpu", weightedCPU)
				log.Info("VPA", "minimum CPU delta", minDeltaCPU.String(), "minimum memory delta", minDeltaMem)
				if diffMem.Sign() > 0 && diffMem.Cmp(*minDeltaMem) > 0 && lastScaleTimeDuration > scaleUpDelay {
					// If the difference is greater than minimum delta
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
					resourceChange = true
				} else if diffMem.Sign() < 0 && negDiffMem.Cmp(*minDeltaMem) > 0 && lastScaleTimeDuration > scaleDownDelay {
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceMemory] = weightedMem.DeepCopy()
					resourceChange = true
				}
				if diffCPU.Sign() > 0 && diffCPU.Cmp(*minDeltaCPU) > 0 && lastScaleTimeDuration > scaleUpDelay {
					// If the difference is greater than minimum delta
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
					resourceChange = true
				} else if diffCPU.Sign() < 0 && negDiffCPU.Cmp(*minDeltaCPU) > 0 && lastScaleTimeDuration > scaleDownDelay {
					newDeploy.Spec.Template.Spec.Containers[id].Resources.Requests[corev1.ResourceCPU] = weightedCPU.DeepCopy()
					resourceChange = true
				}
				// TODO: Add conditions for other resources also: ResourceStorage, ResourceEphemeralStorage,
				break
			}
		}
	}
	log.Info("VPA", "vpa recommends changes?", resourceChange)
	if resourceChange {
		return newDeploy, resourceChange, nil
	}
	return nil, false, nil
}

func getThreshold(thresholdVals *autoscalingv1alpha1.ChangeThreshold, resourceType corev1.ResourceName, currentVal resource.Quantity) (*resource.Quantity, error) {
	var quantity resource.Quantity
	if thresholdVals == nil {
		if resourceType == corev1.ResourceMemory {
			// Default to 200M
			quantity, _ = resource.ParseQuantity("200M")
		}
		if resourceType == corev1.ResourceCPU {
			// Default to 200m
			quantity, _ = resource.ParseQuantity("200m")
		}
		return &quantity, nil
	}

	if thresholdVals.Percentage == 0 {
		quantity = resource.MustParse(thresholdVals.Value)
		return &quantity, nil
	}

	percentageValue := currentVal.ScaledValue(-3) * int64(thresholdVals.Percentage) / 100
	percentageQuantity := resource.NewQuantity(percentageValue, currentVal.Format)
	percentageQuantity.SetScaled(percentageQuantity.Value(), -3)

	if thresholdVals.Value == "" {
		return percentageQuantity, nil
	}

	absoluteQuantity := resource.MustParse(thresholdVals.Value)
	if percentageQuantity.Cmp(absoluteQuantity) < 0 {
		return percentageQuantity, nil
	}
	return &absoluteQuantity, nil
}
