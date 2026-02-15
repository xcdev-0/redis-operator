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
	"fmt"
	"reflect"
	"time"

	"github.com/avast/retry-go"
	intctrlutil "github.com/xcdev-0/redis-operator/internal/controllerutil"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	redisclusterv1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/cluster"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// RedisClusterReconciler reconciles a RedisCluster object
type RedisClusterReconciler struct {
	client.Client
	*statefulset.StatefulSetService
	K8sClient kubernetes.Interface
	Recorder  record.EventRecorder
}

const (
	RedisClusterFinalizer = "ejlabs.in/finalizer"
)

// +kubebuilder:rbac:groups=ejlabs.in,resources=redisclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ejlabs.in,resources=redisclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ejlabs.in,resources=redisclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	cr := &rcvb2.RedisCluster{}
	err := r.Get(ctx, req.NamespacedName, cr)
	if err != nil {
		return intctrlutil.RequeueECheck(ctx, err, "failed to get redis cluster instance")
	}

	if cr.GetDeletionTimestamp() != nil {
		if err := HandleRedisClusterFinalizer(ctx, r.Client, cr, RedisClusterFinalizer); err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to handle redis cluster finalizer")
		}
		return intctrlutil.Reconciled()
	}

	if shouldSkipReconcile(ctx, cr) {
		return intctrlutil.Reconciled()
	}

	cr.SetDefault()

	if err = addFinalizer(ctx, cr, RedisClusterFinalizer, r.Client); err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to add finalizer")
	}

	// replica count
	desiredLeaderReplicas := cr.Spec.GetReplicaCount("leader")
	desiredFollwerReplicas := cr.Spec.GetReplicaCount("follower")
	desiredTotalReplicas := desiredLeaderReplicas + desiredFollwerReplicas

	// ================================================
	// 1. scale in
	// ================================================
	// 첫번째로 실행하는 이유는 슬롯 이동 → follower 제거 → rebalance 같은 절차를 강제하기 위함
	// 다운스케일 로직 진행하지 않고 아래 2,3 스텝에서 sts 팟을 제거하면 위험!!
	currentLeaderReplicas := r.GetStatefulSetReplicas(ctx, cr.Namespace, cr.Name+"-leader")
	currentFollowerReplicas := r.GetStatefulSetReplicas(ctx, cr.Namespace, cr.Name+"-follower")
	if desiredLeaderReplicas < currentLeaderReplicas {
		if !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-leader") || !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-follower") {
			return intctrlutil.Reconciled()
		}
		if desiredLeaderReplicas <= 0 {
			return intctrlutil.RequeueE(ctx, fmt.Errorf("desired leader replicas must be greater than zero for downscale"), "invalid downscale target")
		}

		stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "leader downscale cleanup")
		if err != nil || !stable {
			return result, err
		}

		// Step 1: 초과 ordinal 노드를 Redis cluster 멤버십에서 제거합니다.
		// current>desired인 동안에는 누락 add-node가 아니라 overflow 정리를 우선합니다.
		lastOrdinalToRemove := currentLeaderReplicas - 1
		firstOrdinalToRemove := desiredLeaderReplicas
		for ordinalToRemove := lastOrdinalToRemove; ordinalToRemove >= firstOrdinalToRemove; ordinalToRemove-- {
			leaderPod := k8smeta.GetPodName(cr.Name, "leader", int(ordinalToRemove))

			// leader pod의 실제 역할이 master면 자기 node-id, replica면 4번째 필드(master-id)를 source로 사용합니다.
			sourceMasterNodeID, err := cluster.GetMasterNodeIDByPod(ctx, r.K8sClient, cr, leaderPod)
			if err != nil {
				logger.Info("failed to resolve source master from overflow leader pod; skipping ordinal",
					"Ordinal", ordinalToRemove,
					"Leader.Pod", leaderPod,
					"Error", err.Error())
				continue
			}
			sourceMasterJoined, err := cluster.IsNodeIDInCluster(ctx, r.K8sClient, cr, sourceMasterNodeID)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to check source master node id cluster membership for overflow ordinal")
			}
			if !sourceMasterJoined {
				logger.Info("source master node id already absent in cluster membership; skipping ordinal",
					"Ordinal", ordinalToRemove,
					"Leader.Pod", leaderPod,
					"Source.Master.NodeID", sourceMasterNodeID)
				continue
			}

			// 대상 master 선택은 ordinal%desiredLeaderReplicas 시작점으로 순회합니다.
			transferMasterNodeID, hasTransferMaster, err := r.pickTransferMasterNodeID(ctx, cr, sourceMasterNodeID, desiredLeaderReplicas, ordinalToRemove)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to pick transfer master node id during downscale")
			}
			if !hasTransferMaster {
				logger.Info("no transfer master candidate found yet for overflow source; waiting",
					"Ordinal", ordinalToRemove,
					"Source.Master.NodeID", sourceMasterNodeID,
					"Desired.Leader.Replicas", desiredLeaderReplicas)
				return intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for transfer master candidate during downscale")
			}

			// follower 인덱스와 leader 인덱스가 지금은 1:1이지만
			// 이후에 다배수 follower 환경을 지원할 때는 1:N이 될 수 있음
			// source master를 기준으로 실제 연결된 follower node-id를 조회하여 제거합니다.
			attachedFollowerNodeIDs, err := cluster.GetFollowerNodeIDsByMasterNodeID(ctx, r.K8sClient, cr, sourceMasterNodeID)
			logger.Info("attached follower node ids", "Attached.Follower.NodeIDs", attachedFollowerNodeIDs)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to get follower node ids by source master node id")
			}
			for _, followerNodeID := range attachedFollowerNodeIDs {
				if err := cluster.RemoveRedisNodeByID(ctx, r.K8sClient, cr, followerNodeID); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to remove attached follower node during downscale")
				}
			}

			if err := cluster.ReshardRedisClusterByNodeID(ctx, r.K8sClient, cr, sourceMasterNodeID, transferMasterNodeID); err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to reshard cluster during leader downscale")
			}

			if err := cluster.RemoveRedisNodeByID(ctx, r.K8sClient, cr, sourceMasterNodeID); err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to remove source master node during downscale")
			}

			if err := cluster.RebalanceRedisCluster(ctx, r.K8sClient, cr); err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to rebalance cluster after downscale node removal")
			}

			logger.Info("completed one overflow leader ordinal cleanup pass",
				"Ordinal", ordinalToRemove,
				"Source.Master.NodeID", sourceMasterNodeID,
				"Transfer.Master.NodeID", transferMasterNodeID,
				"Removed.Attached.Follower.Count", len(attachedFollowerNodeIDs))
			return intctrlutil.RequeueAfter(ctx, time.Second*10, "processed one overflow leader ordinal cleanup")
		}

		// overflow 멤버십 정리는 완료되었고, StatefulSet 축소 허용 여부는
		// 아래 CreateRedis*STS 호출 직전에서 최종 체크합니다.
		logger.Info("overflow cluster membership cleanup pass completed; proceeding to statefulset reconcile",
			"Current.Leader.Replicas", currentLeaderReplicas,
			"Desired.Leader.Replicas", desiredLeaderReplicas)
	}
	// ================================================
	// 2. leader node setup
	// ================================================
	// 1) 처음 생성되는 경우
	// 2) 리더 노드 수가 변경된 경우
	if (cr.Status.ReadyLeaderReplicas == 0 && cr.Status.ReadyFollowerReplicas == 0) ||
		cr.Status.ReadyLeaderReplicas != desiredLeaderReplicas {
		recueue, err := r.updateStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterInitializing,
			Reason:                rcvb2.InitializingClusterLeaderReason,
			ReadyLeaderReplicas:   cr.Status.ReadyLeaderReplicas,
			ReadyFollowerReplicas: cr.Status.ReadyFollowerReplicas,
		})
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to update status")
		}
		if recueue {
			return intctrlutil.Requeue()
		}
	}

	if desiredLeaderReplicas > 0 {
		err = cluster.CreateRedisLeaderService(ctx, cr, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to create redis leader service")
		}
	}
	if desiredLeaderReplicas < currentLeaderReplicas {
		firstOrdinalToRemove := desiredLeaderReplicas
		lastLeaderOrdinal := currentLeaderReplicas - 1
		downscaleCleanupDone, err := r.isRoleDownscaleClusterCleanupComplete(ctx, cr, firstOrdinalToRemove, lastLeaderOrdinal, "leader", "follower")
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to verify downscale cleanup completion before statefulset reconcile")
		}
		if !downscaleCleanupDone {
			logger.Info("downscale cleanup is not completed yet; delaying StatefulSet replica reconcile",
				"Current.Leader.Replicas", currentLeaderReplicas,
				"Current.Follower.Replicas", currentFollowerReplicas,
				"Desired.Leader.Replicas", desiredLeaderReplicas,
				"Desired.Follower.Replicas", desiredFollwerReplicas)
			return intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for downscale cleanup completion before sts reconcile")
		}
	}
	err = cluster.CreateRedisLeaderSTS(ctx, cr, r.K8sClient)
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to create redis leader")
	}

	// err = cluster.ReconcileRedisPodDisruptionBudget(ctx, instance, "leader", instance.Spec.RedisLeader.PodDisruptionBudget, r.K8sClient)
	// if err != nil {
	// return intctrlutil.RequeueE(ctx, err, "")
	// }

	// leader 팟들이 모두 준비완료 되었을때 실행해야함. 준비되지 않았을 때 실행하면
	// replication 명령이 실패하거나
	// follower가 붙을 master를 못 찾을 수 잇움
	if r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-leader") {
		// Leader StatefulSet은 Ready이지만 CR Status는 아직 업데이트되지 않은 상태
		if (cr.Status.ReadyLeaderReplicas == 0 && cr.Status.ReadyFollowerReplicas == 0) ||
			cr.Status.ReadyFollowerReplicas != desiredFollwerReplicas {
			requeue, err := r.updateStatus(ctx, cr, rcvb2.RedisClusterStatus{
				State:                 rcvb2.RedisClusterInitializing,
				Reason:                rcvb2.InitializingClusterFollowerReason,
				ReadyLeaderReplicas:   desiredLeaderReplicas,           // leader는 준비완료
				ReadyFollowerReplicas: cr.Status.ReadyFollowerReplicas, // follower는 아직...
			})
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "")
			}
			if requeue {
				return intctrlutil.Requeue()
			}
		}

		if desiredFollwerReplicas != 0 {
			err = cluster.CreateRedisFollowerService(ctx, cr, r.K8sClient)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to create redis follower service")
			}
		}
		if desiredFollwerReplicas < currentFollowerReplicas {
			firstOrdinalToRemove := desiredFollwerReplicas
			lastFollowerOrdinal := currentFollowerReplicas - 1
			followerDownscaleCleanupDone, err := r.isRoleDownscaleClusterCleanupComplete(ctx, cr, firstOrdinalToRemove, lastFollowerOrdinal, "follower")
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to verify follower downscale cleanup completion before statefulset reconcile")
			}
			if !followerDownscaleCleanupDone {
				logger.Info("follower downscale cleanup is not completed yet; delaying follower StatefulSet replica reconcile",
					"Current.Follower.Replicas", currentFollowerReplicas,
					"Desired.Follower.Replicas", desiredFollwerReplicas)
				return intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for follower downscale cleanup completion before follower sts reconcile")
			}
		}
		err = cluster.CreateRedisFollowerSTS(ctx, cr, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to create redis follower statefulset")
		}

		// err = cluster.ReconcileRedisPodDisruptionBudget(ctx, instance, "follower", instance.Spec.RedisFollower.PodDisruptionBudget, r.K8sClient)
		// if err != nil {
		// 	return intctrlutil.RequeueE(ctx, err, "")
		// }
	}

	// Leader 또는 Follower StatefulSet이 아직 준비되지 않았으면 여기서 종료합니다.
	if !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-leader") || !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-follower") {
		return intctrlutil.Reconciled()
	}

	// ================================================
	// 3. bootstrap
	// ================================================
	// statefulset pod들이 준비되었고 cr.status가 업데이트해야 합니다
	if cr.Status.ReadyLeaderReplicas != desiredLeaderReplicas || cr.Status.ReadyFollowerReplicas != desiredFollwerReplicas {
		requeue, err := r.updateStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterBootstrap,
			Reason:                rcvb2.BootstrapClusterReason,
			ReadyLeaderReplicas:   desiredLeaderReplicas,
			ReadyFollowerReplicas: desiredFollwerReplicas, //팔로워들도 준비 완료 !
		})
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "")
		}
		if requeue {
			return intctrlutil.Requeue()
		}
	}

	// ================================================
	// 4. 단일 노드일 때 leader-0 노드에 모든 슬롯 할당 (테스트용)
	// ================================================
	// 아직 클러스터 안에 포함되지는 않음
	// if desiredLeaderReplicas == 1 {
	// 	if slotsAssigned, err := cluster.CheckClusterAllSlotsAssigned(ctx, r.K8sClient, cr); err != nil {
	// 		return intctrlutil.RequeueE(ctx, err, "failed to get cluster slots")
	// 	} else {
	// 		if !slotsAssigned {
	// 			// cluster reset + cluster addslots 실행
	// 			logger.Info("Start creating a single-node redis cluster")
	// 			cluster.AddAllSlotsToSingleNode(ctx, r.K8sClient, cr)
	// 		}
	// 	}
	// }

	// 어드미션 웹훅으로 리더 노드 최소 1개 또는 최소 3개이상으로 강제할거임
	// ================================================
	// 5. 클러스터 초기화
	// ================================================
	// 목표 leader, follower 수에 맞게 클러스터를 초기화합니다.
	// step 5는 유저가 redicluster replicas수를 변경하였을 때 또는 처음 초기화될 때 실행됩니다.
	// noaddr, handshake같은 임시 엔트리를 제외하기 위하여 master, slave 플래그가 있는 노드만 카운트합니다.
	nodeCount, err := cluster.GetClusterNodeCount(ctx, r.K8sClient, cr)
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to get cluster node count")
	}
	if nodeCount != desiredTotalReplicas {
		logger.Info("Creating redis cluster by executing cluster creation commands")
		// 실제로 레디스 클러스터내에서 마스터 역할을 하는 개수
		// healthyLeaderCount와 달리 fail, fail?, disconnected 상태의 노드도 카운트합니다.
		leaderNodeCount, err := cluster.GetClusterMasterNodeCount(ctx, r.K8sClient, cr)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to get cluster master node count")
		}
		joinedLeaderPodCount, err := r.getJoinedRolePodCount(ctx, cr, "leader", desiredLeaderReplicas)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to get joined leader pod count")
		}
		needLeaderJoin := joinedLeaderPodCount < desiredLeaderReplicas
		// TODO: currentLeaderCount > desiredLeaderReplica인경우도 처리해야할까?
		if leaderNodeCount != desiredLeaderReplicas || needLeaderJoin {
			logger.Info("Not all leader are part of the cluster...",
				"Leaders.Count", leaderNodeCount,
				"Joined.Leader.Pods", joinedLeaderPodCount,
				"Instance.Size", desiredLeaderReplicas)
			if leaderNodeCount <= 1 && joinedLeaderPodCount <= 1 {
				// 클러스터 미형성 상태: leader-0만 자기 자신을 master로 인식 (cluster_known_nodes == 1)
				// redis-cli --cluster create로 모든 leader를 포함한 새 클러스터를 생성합니다.
				result, err := cluster.CreateRedisCluster(ctx, r.K8sClient, cr)
				if err != nil {
					logger.Error(err, "failed to create redis cluster")
					return intctrlutil.RequeueE(ctx, err, "failed to create redis cluster")
				}
				logger.Info("Redis cluster creation result", "Result", result)
			} else if needLeaderJoin {
				stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "leader add")
				if err != nil || !stable {
					return result, err
				}

				// 클러스터가 이미 형성된 상태에서 스케일 업: 새 leader 노드를 기존 클러스터에 추가합니다.
				// redis-cli --cluster add-node로 개별 노드를 추가한 후 슬롯을 재배분합니다.
				if err := cluster.AddRedisLeaderNodeToCluster(ctx, r.K8sClient, cr, desiredLeaderReplicas); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to add leader node to cluster")
				}
				if err := cluster.RebalanceRedisClusterEmptyMasters(ctx, r.K8sClient, cr); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to rebalance empty masters")
				}
			} else {
				logger.Info("leader master count mismatch without unjoined leader pods; waiting for convergence",
					"Leaders.Count", leaderNodeCount,
					"Joined.Leader.Pods", joinedLeaderPodCount,
					"Instance.Size", desiredLeaderReplicas)
			}
		} else {
			stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "follower replication")
			if err != nil || !stable {
				return result, err
			}

			currentFollowerCount, err := cluster.GetClusterFollowerNodeCount(ctx, r.K8sClient, cr)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to get cluster follower node count")
			}
			if desiredFollwerReplicas == 0 {
				logger.Info("no follower/replicas configured, skipping replication configuration", "Leaders.Count", leaderNodeCount, "Leader.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
			} else if currentFollowerCount < desiredFollwerReplicas {
				logger.Info("All leader are part of the cluster, adding follower/replicas", "Leaders.Count", leaderNodeCount, "Instance.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
				if err := cluster.ExecuteRedisReplicationCommand(ctx, r.K8sClient, cr, desiredFollwerReplicas, desiredLeaderReplicas); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to execute replication command")
				}
			} else if currentFollowerCount > desiredFollwerReplicas {
				logger.Info("follower count is higher than desired; waiting for node cleanup", "Current.Follower.Count", currentFollowerCount, "Desired.Follower.Count", desiredFollwerReplicas)
			} else {
				logger.Info("leader/follower counts match desired, waiting for total cluster node convergence", "Current.Leader.Count", leaderNodeCount, "Desired.Leader.Count", desiredLeaderReplicas, "Current.Follower.Count", currentFollowerCount, "Desired.Follower.Count", desiredFollwerReplicas)
			}

			return intctrlutil.RequeueAfter(ctx, time.Second*60,
				"Redis cluster count is not desired",
				"Current.Count", nodeCount,
				"Desired.Count", desiredTotalReplicas,
				"Current.Leader.Count", leaderNodeCount,
				"Current.Follower.Count", currentFollowerCount,
				"Desired.Leader.Count", desiredLeaderReplicas,
				"Desired.Follower.Count", desiredFollwerReplicas)
		}
	}

	// leader follower 모두 준비되었으면 클러스터 상태를 확인합니다.
	// ================================================
	// 6. 클러스터 상태 확인
	// ================================================
	logger.Info("Number of Redis nodes match desired")
	unhealthyNodeCount, err := cluster.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to determine unhealthy node count in cluster")
	}

	// 비정상 노드가 발견된 경우 (단일 노드 클러스터는 제외)
	if int(desiredTotalReplicas) > 1 && unhealthyNodeCount > 0 {
		// 상태를 Failed로 설정하여 문제가 있음을 표시합니다.
		requeue, err := r.updateStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterFailed,
			Reason:                "RedisCluster has unhealthy nodes",
			ReadyLeaderReplicas:   desiredLeaderReplicas,
			ReadyFollowerReplicas: desiredFollwerReplicas,
		})
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "")
		}
		if requeue {
			return intctrlutil.Requeue()
		}

		// 연결이 끊긴 Master 노드를 복구 시도합니다.
		// 네트워크 문제나 일시적인 장애로 인해 노드가 클러스터에서 분리되었을 수 있습니다.
		logger.Info("healthy leader count does not match desired; attempting to repair disconnected masters")
		if err = cluster.RepairDisconnectedNodes(ctx, r.K8sClient, cr); err != nil {
			logger.Error(err, "failed to repair disconnected masters")
		}

		// 복구 작업 후 비정상 노드 수를 다시 확인합니다.
		// 최대 3회 시도하며, 각 시도 사이에 5초 대기합니다.
		err = retry.Do(func() error {
			nc, nErr := cluster.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
			if nErr != nil {
				return nErr
			}
			if nc == 0 {
				return nil // 성공: 비정상 노드가 없음
			}
			return fmt.Errorf("%d unhealthy nodes", nc) // 실패: 아직 비정상 노드가 있음
		}, retry.Attempts(3), retry.Delay(time.Second*5))

		// 복구가 성공한 경우
		if err == nil {
			logger.Info("repairing unhealthy masters successful, no unhealthy masters left")
			// 30초 후에 다시 확인합니다. 이 시간 동안 클러스터가 안정화됩니다.
			return intctrlutil.RequeueAfter(ctx, time.Second*30, "no unhealthy nodes found after repairing disconnected masters")
		}

		// 복구가 실패한 경우, 비정상 노드 수를 다시 확인합니다.
		unhealthyNodeCount, err = cluster.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to determine unhealthy node count in cluster")
		}

		// 대부분의 노드가 비정상인 경우 (전체 - 1개 이상)
		// 클러스터가 심각하게 손상되었으므로 수동 개입이 필요합니다.
		// "거의 모든 노드가 실패"한 경우를 감지하기 위해서입니다.
		// 3개 중 2개 실패: 자동 복구가 거의 불가능
		// 5개 중 4개 실패: 자동 복구가 거의 불가능
		if int(desiredTotalReplicas) > 1 && int(unhealthyNodeCount) >= int(desiredTotalReplicas)-1 {
			return intctrlutil.RequeueE(ctx, fmt.Errorf("cluster broken: %d/%d nodes unhealthy, manual intervention required", unhealthyNodeCount, desiredTotalReplicas), "")
		}
	}

	// ========================================================================
	// Step 14: Empty Master 노드 확인
	// ========================================================================
	// 모든 노드가 클러스터에 포함되었는지 확인한 후, 슬롯이 할당되지 않은 Empty Master 노드가 있는지 확인합니다.
	// Empty Master는 클러스터에 포함되어 있지만 슬롯이 없는 노드입니다 (스케일 아웃 후 발생할 수 있음).
	if ncCheck, err := cluster.GetClusterNodeCount(ctx, r.K8sClient, cr); err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to get cluster node count")
	} else if ncCheck == desiredTotalReplicas {
		// Empty Master가 발견되면 자동으로 재밸런싱하여 슬롯을 분배합니다.
		cluster.RebalanceIfEmptyMasterExists(ctx, r.K8sClient, cr)
	}

	// ========================================================================
	// Step 15: 클러스터 Ready 상태 설정
	// ========================================================================
	// 모든 Leader와 Follower가 준비되었고, 아직 Ready 상태가 아니면 Ready로 전환합니다.
	// 이미 Ready 상태인 경우 불필요한 상태 업데이트를 방지하기 위해 건너뜁니다.
	// Initializing 상태에서는 Bootstrap을 거쳐야 하므로 Ready로 전환하지 않습니다.
	// fail일때도 기다려야할까?
	if cr.Status.ReadyLeaderReplicas == desiredLeaderReplicas &&
		cr.Status.ReadyFollowerReplicas == desiredFollwerReplicas &&
		cr.Status.State != rcvb2.RedisClusterReady &&
		cr.Status.State != rcvb2.RedisClusterInitializing {
		// 먼저 클러스터가 비정상 상태라고 가정하고 메트릭을 0으로 설정합니다.

		// 클러스터 상태를 확인합니다 (redis-cli --cluster check 명령 사용).
		if cluster.RedisClusterStatusHealth(ctx, r.K8sClient, cr) {

			// 동적 설정을 모든 Redis 인스턴스에 적용합니다.
			// 동적 설정은 CONFIG SET 명령으로 런타임에 변경 가능한 설정입니다.
			if err = cluster.SetRedisClusterDynamicConfig(ctx, r.K8sClient, cr); err != nil {
				logger.Error(err, "Failed to set dynamic config")
				return intctrlutil.RequeueE(ctx, err, "failed to set dynamic config")
			}

			// 상태를 Ready로 업데이트합니다.
			requeue, err := r.updateStatus(ctx, cr, rcvb2.RedisClusterStatus{
				State:                 rcvb2.RedisClusterReady,
				Reason:                rcvb2.ReadyClusterReason,
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
	}

	// ========================================================================
	// Step 16: Pod 라벨 동기화
	// ========================================================================
	// Pod의 실제 역할(leader/follower)을 Kubernetes 라벨로 동기화합니다.
	// 이렇게 하면 Service Selector가 올바르게 작동하여 각 역할의 Pod를 정확히 선택할 수 있습니다.
	if err := cluster.UpdateRedisRoleLabels(ctx, r.K8sClient, cr); err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to update redis role labels")
	}

	// 정상 상태에서도 주기적으로 reconcile하여 클러스터 상태를 모니터링합니다.
	return intctrlutil.RequeueAfter(ctx, time.Second*10, "")
}

func (r *RedisClusterReconciler) pickTransferMasterNodeID(ctx context.Context, cr *rcvb2.RedisCluster, sourceMasterNodeID string, desiredLeaderReplicas, ordinalToRemove int32) (string, bool, error) {
	if desiredLeaderReplicas <= 0 {
		return "", false, fmt.Errorf("desired leader replicas must be greater than zero to pick transfer master")
	}

	startOrdinal := int(ordinalToRemove % desiredLeaderReplicas)
	totalCandidates := int(desiredLeaderReplicas)
	for offset := 0; offset < totalCandidates; offset++ {
		ordinal := (startOrdinal + offset) % totalCandidates
		podName := k8smeta.GetPodName(cr.Name, "leader", ordinal)

		joined, err := cluster.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
		if err != nil {
			return "", false, fmt.Errorf("failed to check cluster membership for transfer pod %s: %w", podName, err)
		}
		if !joined {
			continue
		}

		masterNodeID, err := cluster.GetMasterNodeIDByPod(ctx, r.K8sClient, cr, podName)
		if err != nil {
			return "", false, fmt.Errorf("failed to resolve transfer master node id from pod %s: %w", podName, err)
		}
		if masterNodeID == "" || masterNodeID == sourceMasterNodeID {
			continue
		}
		return masterNodeID, true, nil
	}
	return "", false, nil
}

// ensureClusterStableForMembershipChange guards add-node/del-node style operations.
// It blocks membership changes while cluster has unhealthy or pending(handshake/noaddr) nodes.
func (r *RedisClusterReconciler) ensureClusterStableForMembershipChange(ctx context.Context, cr *rcvb2.RedisCluster, action string) (bool, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	unhealthyNodeCount, err := cluster.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get unhealthy node count before membership change", "Action", action)
		return false, result, requeueErr
	}
	if unhealthyNodeCount > 0 {
		logger.Info("cluster has unhealthy nodes, delaying membership change",
			"Action", action,
			"Unhealthy.Node.Count", unhealthyNodeCount)
		if repairErr := cluster.RepairDisconnectedNodes(ctx, r.K8sClient, cr); repairErr != nil {
			logger.Error(repairErr, "failed to repair disconnected nodes before membership change", "Action", action)
		}
		result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*30, "cluster has unhealthy nodes, waiting before membership change", "Action", action)
		return false, result, requeueErr
	}

	pendingNodeCount, err := cluster.GetClusterPendingNodeCount(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get pending node count before membership change", "Action", action)
		return false, result, requeueErr
	}
	if pendingNodeCount > 0 {
		logger.Info("cluster has pending handshake/noaddr nodes, delaying membership change",
			"Action", action,
			"Pending.Node.Count", pendingNodeCount)
		result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*30, "cluster has pending handshake/noaddr nodes, waiting before membership change", "Action", action)
		return false, result, requeueErr
	}

	return true, ctrl.Result{}, nil
}

func (r *RedisClusterReconciler) getJoinedRolePodCount(ctx context.Context, cr *rcvb2.RedisCluster, role string, desiredReplica int32) (int32, error) {
	if desiredReplica <= 0 {
		return 0, nil
	}

	var joinedCount int32
	for ordinal := 0; ordinal < int(desiredReplica); ordinal++ {
		podName := k8smeta.GetPodName(cr.Name, role, ordinal)
		joined, err := cluster.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
		if err != nil {
			return 0, fmt.Errorf("failed to check %s cluster membership for %s: %w", role, podName, err)
		}
		if joined {
			joinedCount++
		}
	}
	return joinedCount, nil
}

func (r *RedisClusterReconciler) isRoleDownscaleClusterCleanupComplete(ctx context.Context, cr *rcvb2.RedisCluster, firstOrdinal, lastOrdinal int32, roles ...string) (bool, error) {
	logger := log.FromContext(ctx)
	if len(roles) == 0 {
		return true, nil
	}

	for ordinal := lastOrdinal; ordinal >= firstOrdinal; ordinal-- {
		for _, role := range roles {
			podName := k8smeta.GetPodName(cr.Name, role, int(ordinal))
			joined, err := cluster.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
			if err != nil {
				return false, fmt.Errorf("failed to check %s cluster membership for %s: %w", role, podName, err)
			}
			if joined {
				logger.Info("overflow ordinal still joined in cluster membership",
					"Ordinal", ordinal,
					"Role", role,
					"Pod", podName,
					"Joined", joined)
				return false, nil
			}
		}
	}
	return true, nil
}

// deleteOrphanedPVCs deletes PVCs left behind after downscale for the given ordinal.
// Both leader and follower PVCs (node-conf, data-persistence) are deleted.
// NotFound errors are ignored (already deleted). Other errors are returned but non-fatal.
func (r *RedisClusterReconciler) deleteOrphanedPVCs(ctx context.Context, cr *rcvb2.RedisCluster, ordinal int) error {
	logger := log.FromContext(ctx)
	roles := []string{"leader", "follower"}
	volumeNames := []string{consts.VolumeNameNodeConf, consts.VolumeNameData}

	for _, role := range roles {
		podName := k8smeta.GetPodName(cr.Name, role, ordinal)
		for _, volName := range volumeNames {
			pvcName := fmt.Sprintf("%s-%s", volName, podName)
			pvc := &corev1.PersistentVolumeClaim{}
			pvc.Name = pvcName
			pvc.Namespace = cr.Namespace
			if err := r.Client.Delete(ctx, pvc); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				logger.Error(err, "failed to delete PVC", "pvc", pvcName)
				return err
			}
			logger.Info("deleted orphaned PVC", "pvc", pvcName)
		}
	}
	return nil
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
