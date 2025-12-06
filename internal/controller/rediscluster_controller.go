package controller

import (
	"context"

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
	redisv1alpha1 "github.com/xcdev-0/redis-operator/api/v1alpha1"
	k8sutils "github.com/xcdev-0/redis-operator/k8sutils"
)

type RedisClusterReconciler struct {
	client.Client
	K8sClient kubernetes.Interface
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	clusterHost, err := k8sutils.GetCluster(ctx, r.K8sClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Cluster", "cluster", clusterHost)
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
