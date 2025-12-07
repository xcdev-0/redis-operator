package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	redisv1alpha1 "github.com/xcdev-0/redis-operator/api/v1alpha1"
	"github.com/xcdev-0/redis-operator/k8sutils"
)

type RedisClusterReconciler struct {
	client.Client
	K8sClient kubernetes.Interface
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *RedisClusterReconciler) ensurePod(
	ctx context.Context,
	rc *redisv1alpha1.RedisCluster,
	name string,
	index int,
) error {
	log := r.Log.WithValues(
		"rediscluster", rc.Namespace+"/"+rc.Name,
		"pod", name,
		"index", index,
	)

	// 1) 이미 존재하는지 확인
	existing := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: rc.Namespace,
	}, existing)

	if err == nil {
		// 이미 존재 → 아무것도 안 함
		log.Info("Pod already exists, skip create")
		return nil
	}
	if !errors.IsNotFound(err) {
		// 진짜 에러
		log.Error(err, "Failed to get Pod")
		return err
	}

	// 2) 없으니까 새로 생성
	newPod := k8sutils.GenerateRedisPodDef(rc, name, index)

	log.Info("Creating new Pod")
	if err := r.Create(ctx, newPod); err != nil {
		// 경쟁 상태: 다른 Reconcile이 먼저 만들어버리면 AlreadyExists 나올 수 있음
		if errors.IsAlreadyExists(err) {
			log.Info("Pod already created by another reconcile, ok")
			return nil
		}
		log.Error(err, "Failed to create Pod")
		return err
	}

	return nil
}

func (r *RedisClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redisv1alpha1.RedisCluster{}).
		Owns(&corev1.Pod{}). // Pod 생성/삭제 시 자동으로 RedisCluster Reconcile 호출
		Complete(r)
}
