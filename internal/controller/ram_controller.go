/*
Copyright 2023.

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

package controller

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	batchv1 "tutorial.kubebuilder.io/project/api/v1"
)

// RamReconciler reconciles a Ram object
type RamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=rams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=rams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=rams/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=pods/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Ram object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.0/pkg/reconcile
func (r *RamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	log.Log.Info("into this for reconciling")

	// get the ram resource
	var ram batchv1.Ram
	if err := r.Get(ctx, req.NamespacedName, &ram); err != nil {
		log.Log.Error(err, "unable to fetch CronJob")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// get the pods in the resource
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(req.Namespace), client.MatchingFields{podOwnerKey: req.Name}); err != nil {
		log.Log.Error(err, "Unable to fetch the podList!")
	}

	// Get the active and inactive pods.

	isPodFinished := func(pod *corev1.Pod) bool {
		for _, c := range pod.Status.ContainerStatuses {
			if c.LastTerminationState != (corev1.ContainerState{}) && c.LastTerminationState.Terminated.Reason == "Completed" {
				return true
			}
		}

		return false
	}
	log.Log.Info("came till here for reconciling")
	var active, inactive []*corev1.Pod
	for _, Pod := range podList.Items {
		if isPodFinished(&Pod) {
			inactive = append(inactive, &Pod)
			log.Log.Info("Appended  to inactive pod", Pod)
		} else {
			active = append(active, &Pod)
			log.Log.Info("Appended  to active pod", Pod)
		}
	}

	log.Log.Info("came till here for reconciling 2")
	// Remove the inactive pods and delete them
	for _, pod := range inactive {
		if err := r.Delete(ctx, pod, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			log.Log.Error(err, "unable to delete completed pod", "pod", pod)
		} else {
			log.Log.Info("deleted old pod", "pod", pod)
		}
	}

	// schedule new pods if countOfPods != activePods
	count := int(*ram.Spec.PodCount) - len(active)
	if count != 0 {
		constructPods(ctx, count, active, &ram, r)
	}

	// store the pods as active jobs in status
	for _, activePod := range active {
		jobRef, err := ref.GetReference(r.Scheme, activePod)
		if err != nil {
			log.Log.Error(err, "Unable to make reference for a active pod", "pod", activePod)
			continue
		}
		ram.Status.Active = append(ram.Status.Active, *jobRef)
	}

	return ctrl.Result{}, nil
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func constructPods(ctx context.Context, cnt int, active []*corev1.Pod, ram *batchv1.Ram, r *RamReconciler) {
	log.Log.Info("came till here for reconciling 3" + strconv.Itoa(cnt))
	for i := 0; i < cnt; i++ {
		name := fmt.Sprintf("%s-%s", ram.Name, RandStringBytes(5))

		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
				Name:        name,
				Namespace:   ram.Namespace,
			},
			Spec: *ram.Spec.PodTemplate.Spec.DeepCopy(),
		}

		for k, v := range ram.Spec.PodTemplate.Annotations {
			newPod.Annotations[k] = v
		}

		for k, v := range ram.Spec.PodTemplate.Labels {
			newPod.Labels[k] = v
		}

		if err := ctrl.SetControllerReference(ram, newPod, r.Scheme); err != nil {
			log.Log.Error(err, "Error creating a new pod", "pod", newPod.Name)
		} else {
			active = append(active, newPod)
		}

		if err := r.Create(ctx, newPod); err != nil {
			log.Log.Error(err, "unable to create the pod", "pod", newPod.Name)
		}

		log.Log.Info("created the pod", "pod", newPod.Name)
	}

}

// SetupWithManager sets up the controller with the Manager.
func (r *RamReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, podOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		job := rawObj.(*corev1.Pod)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}

		log.Log.Info("came in here as somechange detected")

		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVStr || owner.Kind != "Ram" {
			log.Log.Info(owner.Kind)
			log.Log.Info(owner.APIVersion)
			log.Log.Info("came in here as irrelavent")
			return nil
		}

		log.Log.Info("Triggered")
		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Ram{}).
		Complete(r)
}

var podOwnerKey = ".metadata.controller"
var apiGVStr = batchv1.GroupVersion.String()
