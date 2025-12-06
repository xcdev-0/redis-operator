package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	redisv1alpha1 "github.com/myname/my-redis-operator/api/v1alpha1"
)

type RedisClusterReconciler struct {
	client.Client
	K8sClient kubernetes.Interface
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cluster := &redisv1alpha1.RedisCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	desiredMasters := cluster.Spec.Masters
	desiredReplicas := cluster.Spec.Replicas
	totalDesired := desiredMasters + desiredMasters*desiredReplicas

	var readyCount int

	// Create master pods
	for i := 0; i < desiredMasters; i++ {
		podName := fmt.Sprintf("%s-master-%d", cluster.Name, i)
		if err := r.ensurePod(ctx, cluster, podName); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create replica pods
	for m := 0; m < desiredMasters; m++ {
		for rIndex := 0; rIndex < desiredReplicas; rIndex++ {
			podName := fmt.Sprintf("%s-replica-%d-%d", cluster.Name, m, rIndex)
			if err := r.ensurePod(ctx, cluster, podName); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Count ready pods
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(cluster.Namespace),
		client.MatchingLabels{"rediscluster": cluster.Name}); err != nil {
		return ctrl.Result{}, err
	}

	for _, p := range podList.Items {
		for _, cond := range p.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				readyCount++
			}
		}
	}

	// Update status
	if cluster.Status.ReadyNodes != readyCount {
		cluster.Status.ReadyNodes = readyCount
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	if readyCount != totalDesired {
		return ctrl.Result{RequeueAfter: 5 * 1000000000}, nil
	}

	log.Info("All pods created and ready", "ready", readyCount)
	return ctrl.Result{}, nil
}

func (r *RedisClusterReconciler) ensurePod(ctx context.Context, cluster *redisv1alpha1.RedisCluster, podName string) error {
	existing := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"rediscluster": cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cluster.APIVersion,
					Kind:       cluster.Kind,
					Name:       cluster.Name,
					UID:        cluster.UID,
					Controller: func(b bool) *bool { return &b }(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "redis",
					Image: "redis:7.0",
					Command: []string{
						"redis-server",
						"--cluster-enabled",
						"yes",
						"--port",
						"6379",
					},
				},
			},
		},
	}

	return r.Create(ctx, pod)
}

func (r *RedisClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redisv1alpha1.RedisCluster{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
