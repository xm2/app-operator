package appservice

import (
	"context"
	"fmt"
	"reflect"

	appv1alpha1 "app-operator/pkg/apis/app/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const randomSuffixLength = 10

// MaxNameLength define the maximum length of k8s object name
const MaxNameLength = 63 - randomSuffixLength - 1

var log = logf.Log.WithName("controller_appservice")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AppService Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAppService{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("appservice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AppService
	err = c.Watch(&source.Kind{Type: &appv1alpha1.AppService{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner AppService
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appv1alpha1.AppService{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAppService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAppService{}

// ReconcileAppService reconciles a AppService object
type ReconcileAppService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a AppService object and makes changes based on the state read
// and what is in the AppService.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAppService) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling AppService")

	// Fetch the AppService instance
	instance := &appv1alpha1.AppService{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	val := instance.Spec.Values.Objects
	reqLogger.Info("Spec.Values", "type", reflect.TypeOf(val), "value:", val)
	for k, v := range val {
		switch t := v.(type) {
		default:
			reqLogger.Info("Spec.Values", "type", reflect.TypeOf(t), "key:", k, "value:", t)
		}
	}
	/*
		if m, ok := val["m"]; ok {
			switch tm := m.(type) {
			case map[string]interface{}:
				for k, v := range tm {
					switch t := v.(type) {
					case string:
						reqLogger.Info("type string", "key", k, "value:", t)
					case bool:
						reqLogger.Info("type bool", "key", k, "value:", t)
					case int64:
						reqLogger.Info("type int64", "key", k, "value:", t)
					default:
						fmt.Printf("unkown type %T, key: %+v, value:%+v", t, k, t)

					}
				}
			}

		}
	*/

	podList := &corev1.PodList{}
	listOpts := &client.ListOptions{Namespace: instance.Namespace}
	listOpts.SetLabelSelector(fmt.Sprintf("app=%s", instance.Name))
	err = r.client.List(context.TODO(), listOpts, podList)
	if err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	running := []*corev1.Pod{}
	pending := []*corev1.Pod{}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if len(pod.OwnerReferences) < 1 {
			continue
		}
		if pod.OwnerReferences[0].UID != instance.UID {
			continue
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			running = append(running, pod)
		case corev1.PodPending:
			pending = append(pending, pod)
		}
	}

	replicas := int32(len(running) + len(pending))
	diff := int(*instance.Spec.Replicas - replicas)
	reqLogger.Info("reconcile:", "Spec.replicas", *instance.Spec.Replicas, "pod created replicas", replicas)
	// scale out
	for diff > 0 {
		reqLogger.Info("reconcile: scale out")
		pod := newPodForCRWithRandomName(instance)
		// Set AppService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}
		diff--
	}

	if diff < 0 {
		// scale in
		// firstly chose from pending list, then from running list, simply chose the last one in the return list
		reqLogger.Info("reconcile: scale in")
		delList := append(running, pending...)
		delList = delList[(len(delList) + diff):]
		for _, p := range delList {
			err = r.client.Delete(context.TODO(), p, client.GracePeriodSeconds(5))
			reqLogger.Info("reconcile: scale in pod", "Pod.Namespace", p.Namespace, "Pod.Name", p.Name)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	podList = &corev1.PodList{}
	listOpts = &client.ListOptions{Namespace: instance.Namespace}
	listOpts.SetLabelSelector(fmt.Sprintf("app=%s", instance.Name))
	err = r.client.List(context.TODO(), listOpts, podList)
	if err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	instance.Status.Replicas = int32(len(podList.Items))

	err = r.client.Status().Update(context.TODO(), instance)
	reqLogger.Info("reconcile: status update", "Pod.Namespace", instance.Namespace, "Pod.Name", instance.Name,
		"status replicas", instance.Status.Replicas)
	if err != nil {
		return reconcile.Result{}, err
	}
	/*
		// Define a new Pod object
		pod := newPodForCR(instance)

		// Set AppService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		// Check if this Pod already exists
		found := &corev1.Pod{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
			err = r.client.Create(context.TODO(), pod)
			if err != nil {
				return reconcile.Result{}, err
			}

			// Pod created successfully - don't requeue
			return reconcile.Result{}, nil
		} else if err != nil {
			return reconcile.Result{}, err
		}

		// Pod already exists - don't requeue
		reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	*/
	return reconcile.Result{}, nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *appv1alpha1.AppService) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-pod",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}

func uniqueMemberName(Name string) string {
	suffix := utilrand.String(randomSuffixLength)
	if len(Name) > MaxNameLength {
		Name = Name[:MaxNameLength]
	}
	return Name + "-" + suffix
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCRWithRandomName(cr *appv1alpha1.AppService) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uniqueMemberName(cr.Name),
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}
