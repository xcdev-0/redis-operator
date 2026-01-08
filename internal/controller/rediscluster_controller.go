/*
Copyright 2025.

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
	"reflect"
	"time"

	intctrlutil "github.com/xcdev-0/redis-operator/internal/controllerutil"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	redisclusterv1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/cluster"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// RedisClusterReconciler reconciles a RedisCluster object
type RedisClusterReconciler struct {
	client.Client
	K8sClient kubernetes.Interface
	Healer    cluster.Healer
	Checker   cluster.Checker
	Recorder  record.EventRecorder
	*statefulset.StatefulSetService
	Scheme *runtime.Scheme
}

const (
	RedisClusterFinalizer = "redis.ejlabs.in/finalizer"
)

// +kubebuilder:rbac:groups=cluster.redisutils.ejlabs.in,resources=redisclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redisutils.ejlabs.in,resources=redisclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redisutils.ejlabs.in,resources=redisclusters/finalizers,verbs=update

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	instance := &rcvb2.RedisCluster{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		return intctrlutil.RequeueECheck(ctx, err, "failed to get redis cluster instance")
	}

	if instance.GetDeletionTimestamp() != nil {
		if err := HandleRedisClusterFinalizer(ctx, r.Client, instance, RedisClusterFinalizer); err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to handle redis cluster finalizer")
		}
		return intctrlutil.Reconciled()
	}

	if shouldSkipReconcile(ctx, instance) {
		return intctrlutil.Reconciled()
	}

	instance.SetDefault()

	if err = addFinalizer(ctx, instance, RedisClusterFinalizer, r.Client); err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to add finalizer")
	}

	// replica count
	desiredLeaderReplicas := instance.Spec.GetLeaderReplicaCount()
	desiredFollwerReplicas := instance.Spec.GetFollowerReplicaCount()
	desiredTotalReplicas := desiredLeaderReplicas + desiredFollwerReplicas

	// ================================================
	// 1. downscale
	// ================================================

	// ================================================
	// 2. leader node setup
	// ================================================
	// 1) 처음 생성되는 경우
	// 2) 리더 노드 수가 변경된 경우
	if (instance.Status.ReadyLeaderReplicas == 0 && instance.Status.ReadyFollowerReplicas == 0) ||
		instance.Status.ReadyLeaderReplicas != desiredLeaderReplicas {
		recueue, err := r.updateStatus(ctx, instance, rcvb2.RedisClusterStatus{
			ReadyLeaderReplicas:   instance.Status.ReadyLeaderReplicas,
			ReadyFollowerReplicas: instance.Status.ReadyFollowerReplicas,
			State:                 rcvb2.RedisClusterInitializing,
			Reason:                rcvb2.InitializingClusterLeaderReason,
		})
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to update status")
		}
		if recueue {
			return intctrlutil.Requeue()
		}
	}

	err = cluster.CreateRedisLeader(ctx, instance, r.K8sClient)
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to create redis leader")
	}

	if desiredLeaderReplicas > 0 {
		err = cluster.CreateRedisLeaderService(ctx, instance, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to create redis follower")
		}
	}

	// err = cluster.ReconcileRedisPodDisruptionBudget(ctx, instance, "leader", instance.Spec.RedisLeader.PodDisruptionBudget, r.K8sClient)
	// if err != nil {
	// 	return intctrlutil.RequeueE(ctx, err, "")
	// }

	if r.IsStatefulSetReady(ctx, instance.Namespace, instance.Name+"-leader") {
		// Leader StatefulSet은 Ready이지만 CR Status는 아직 업데이트되지 않은 상태
		if (instance.Status.ReadyLeaderReplicas == 0 && instance.Status.ReadyFollowerReplicas == 0) ||
			instance.Status.ReadyFollowerReplicas != desiredFollwerReplicas {
			requeue, err := r.updateStatus(ctx, instance, rcvb2.RedisClusterStatus{
				State:                 rcvb2.RedisClusterInitializing,
				Reason:                rcvb2.InitializingClusterFollowerReason,
				ReadyLeaderReplicas:   instance.Status.ReadyLeaderReplicas,
				ReadyFollowerReplicas: instance.Status.ReadyFollowerReplicas,
			})
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "")
			}
			if requeue {
				return intctrlutil.Requeue()
			}
		}

		err = cluster.CreateRedisFollower(ctx, instance, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "")
		}

		if desiredFollwerReplicas != 0 {
			err = cluster.CreateRedisFollowerService(ctx, instance, r.K8sClient)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "")
			}
		}

		// err = cluster.ReconcileRedisPodDisruptionBudget(ctx, instance, "follower", instance.Spec.RedisFollower.PodDisruptionBudget, r.K8sClient)
		// if err != nil {
		// 	return intctrlutil.RequeueE(ctx, err, "")
		// }
	}

	// Leader 또는 Follower StatefulSet이 아직 준비되지 않았으면 여기서 종료합니다.
	if !r.IsStatefulSetReady(ctx, instance.Namespace, instance.Name+"-leader") || !r.IsStatefulSetReady(ctx, instance.Namespace, instance.Name+"-follower") {
		return intctrlutil.Reconciled()
	}

	// ================================================
	// 3. bootstrap
	// ================================================
	// statefulset pod들이 준비되었지만 status가 업데이트되지 않은 경우
	if instance.Status.ReadyLeaderReplicas != desiredLeaderReplicas || instance.Status.ReadyFollowerReplicas != desiredFollwerReplicas {
		requeue, err := r.updateStatus(ctx, instance, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterBootstrap,
			Reason:                rcvb2.BootstrapClusterReason,
			ReadyLeaderReplicas:   desiredLeaderReplicas,
			ReadyFollowerReplicas: desiredFollwerReplicas,
		})
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "")
		}
		if requeue {
			return intctrlutil.Requeue()
		}
	}

	// ================================================
	// 4. 단일 노드
	// ================================================
	if desiredLeaderReplicas == 1 {
		// 슬롯이 할당되었는지 확인합니다.
		// 슬롯이 할당되지 않았으면 클러스터 초기화 명령을 실행합니다.
		if slotsAssigned, err := r.Checker.CheckClusterSlotsAssigned(ctx, instance); err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to get cluster slots")
		} else {
			if !slotsAssigned {
				logger.Info("Start creating a single-node redis cluster")
				// 단일 노드 클러스터를 초기화합니다.
				// 이 명령은 모든 슬롯을 단일 노드에 할당합니다.
				cluster.ExecuteRedisClusterCommand(ctx, r.K8sClient, instance)
			}
		}
	}

	// ================================================
	// 5. 다중 노드 클러스터 초기화
	// ================================================
	if nc := cluster.CheckRedisNodeCount(ctx, r.K8sClient, instance, ""); nc != desiredTotalReplicas {
		logger.Info("Creating redis cluster by executing cluster creation commands")
		clusterLeaderNodeCnt := cluster.CheckRedisNodeCount(ctx, r.K8sClient, instance, "leader")

		// Leader 노드가 클러스터에 모두 포함되지 않은 경우
		if clusterLeaderNodeCnt != desiredLeaderReplicas {
			logger.Info("Not all leader are part of the cluster...", "Leaders.Count", clusterLeaderNodeCnt, "Instance.Size", desiredLeaderReplicas)

			// Leader가 2개 이하인 경우: 초기 클러스터 생성
			// Redis Cluster는 최소 3개의 노드가 필요하지만, 초기 생성 단계에서는 2개 이하일 수 있습니다.
			if clusterLeaderNodeCnt <= 2 {
				// 클러스터 초기화 명령을 실행합니다.
				// 이 명령은 모든 Leader 노드를 클러스터에 추가하고 슬롯을 분배합니다.
				cluster.ExecuteRedisClusterCommand(ctx, r.K8sClient, instance)
			} else {
				// Leader가 3개 이상이지만 desired 개수보다 적은 경우: 스케일 아웃
				if clusterLeaderNodeCnt < desiredLeaderReplicas {
					// Step 1: 새 Leader 노드를 클러스터에 추가합니다.
					// 새로 생성된 Pod는 아직 클러스터에 포함되지 않은 "empty master" 상태입니다.
					cluster.AddRedisNodeToCluster(ctx, r.K8sClient, instance)
					// monitoring.RedisClusterAddingNodeAttempt.WithLabelValues(instance.Namespace, instance.Name).Inc()

					// Step 2: Empty master 노드를 사용하여 클러스터를 재밸런싱합니다.
					// 새로 추가된 노드에 슬롯을 분배하여 부하를 분산시킵니다.
					cluster.RebalanceRedisClusterEmptyMasters(ctx, r.K8sClient, instance)
				}
			}
		} else {
			// 모든 Leader가 클러스터에 포함된 경우: Follower 추가
			if desiredFollwerReplicas > 0 {
				logger.Info("All leader are part of the cluster, adding follower/replicas", "Leaders.Count", clusterLeaderNodeCnt, "Instance.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
				// Follower 노드를 각 Leader에 복제본으로 연결합니다.
				// 이 명령은 Follower를 Leader에 연결하고 데이터 복제를 시작합니다.
				cluster.ExecuteRedisReplicationCommand(ctx, r.K8sClient, instance)
			} else {
				logger.Info("no follower/replicas configured, skipping replication configuration", "Leaders.Count", clusterLeaderNodeCnt, "Leader.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
			}
		}

		// 클러스터 초기화 작업이 진행 중이므로 60초 후에 다시 reconcile합니다.
		// 이 시간 동안 노드가 클러스터에 추가되고 안정화됩니다.
		return intctrlutil.RequeueAfter(ctx, time.Second*60, "Redis cluster count is not desired", "Current.Count", nc, "Desired.Count", desiredTotalReplicas)
	}

	return ctrl.Result{}, nil
}

func (r *RedisClusterReconciler) updateStatus(ctx context.Context, rc *rcvb2.RedisCluster, status rcvb2.RedisClusterStatus) (requeue bool, err error) {
	// 상태가 변경되지 않았으면 업데이트할 필요가 없습니다.
	// DeepEqual을 사용하여 모든 필드를 비교합니다.
	if reflect.DeepEqual(rc.Status, status) {
		return false, nil
	}

	// 리소스의 복사본을 만들어서 Status만 업데이트합니다.
	// Spec은 빈 구조체로 설정하여 Status만 업데이트하도록 합니다.
	copy := rc.DeepCopy()
	copy.Spec = rcvb2.RedisClusterSpec{} // Spec은 업데이트하지 않음
	copy.Status = status                 // Status만 업데이트
	err = r.Client.Status().Update(ctx, copy)
	// Conflict 에러가 발생한 경우 (다른 프로세스가 동시에 업데이트한 경우)
	if err != nil && apierrors.IsConflict(err) {
		log.FromContext(ctx).Info("conflict detected, reloading instance and retrying status update")
		// 최신 버전의 리소스를 다시 가져옵니다.
		namespacedName := client.ObjectKey{
			Namespace: rc.Namespace,
			Name:      rc.Name,
		}
		if err := r.Get(ctx, namespacedName, rc); err != nil {
			return true, err
		}
		// 다시 복사본을 만들어서 Status를 업데이트합니다.
		copy = rc.DeepCopy()
		copy.Spec = rcvb2.RedisClusterSpec{}
		copy.Status = status
		// 재시도하므로 requeue를 true로 반환합니다.
		return true, r.Client.Status().Update(ctx, copy)
	}
	// Conflict가 아닌 다른 에러가 발생한 경우 에러를 반환합니다. 이게 맛ㅅㅅㅅ나ㅏㅏㅏㅏ
	if err != nil {
		return false, err
	}
	// Status 업데이트가 성공한 경우
	return false, nil
}

func shouldSkipReconcile(ctx context.Context, obj metav1.Object) (skip bool) {
	// defer func() {
	// 	if skip {
	// 		log.FromContext(ctx).Info("found skip reconcile annotation", "namespace", obj.GetNamespace(), "name", obj.GetName())
	// 	}
	// }()
	// annotations := obj.GetAnnotations()
	// if annotations == nil {
	// 	return false
	// }
	// monitoring.RedisClusterSkipReconcile.WithLabelValues(obj.GetNamespace(), obj.GetName()).Set(0)
	// if value, found := annotations[RedisClusterSkipReconcileAnnotation]; found && value == "true" {
	// 	monitoring.RedisClusterSkipReconcile.WithLabelValues(obj.GetNamespace(), obj.GetName()).Set(1)
	// 	return true
	// }

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisClusterReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&redisclusterv1beta2.RedisCluster{}). // RedisCluster CRD를 관찰
		Owns(&appsv1.StatefulSet{}).              // StatefulSet을 소유
		Named("cluster").
		Complete(r)
}
