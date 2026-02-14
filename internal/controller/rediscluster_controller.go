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
	if desiredLeaderReplicas < currentLeaderReplicas {
		if !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-leader") || !r.IsStatefulSetReady(ctx, cr.Namespace, cr.Name+"-follower") {
			return intctrlutil.Reconciled()
		}

		// 실제 Redis 클러스터에 있는 Leader 노드 수를 확인합니다.
		// StatefulSet의 리플리카 수와 실제 클러스터의 노드 수가 일치해야 다운스케일을 진행합니다.
		realRedisNodeCount, err := cluster.GetClusterMasterNodeCount(ctx, r.K8sClient, cr)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to get cluster master node count")
		}
		if realRedisNodeCount == currentLeaderReplicas {
			// Kubernetes Event를 기록하여 다운스케일 시작을 알립니다.
			r.Recorder.Event(cr, corev1.EventTypeNormal, rcvb2.EventReasonRedisClusterDownscale, "Redis cluster is downscaling...")
			logger.Info("Redis cluster is downscaling...", "Current.LeaderReplicas", currentLeaderReplicas, "Desired.LeaderReplicas", desiredLeaderReplicas)

			// 제거할 샤드를 역순으로 처리합니다 (마지막 샤드부터 제거).
			// 예: 5개에서 3개로 줄일 때, 샤드 4, 3을 순서대로 제거합니다.
			lastLeaderOrdinal := currentLeaderReplicas - 1
			firstOrdinalToRemove := desiredLeaderReplicas
			for leaderOrdinal := lastLeaderOrdinal; leaderOrdinal >= firstOrdinalToRemove; leaderOrdinal-- {
				logger.Info("Remove the Pod", "Pod.Index", leaderOrdinal)

				// 중요: Kubernetes의 "leader" StatefulSet에 속한 Pod라도, 실제 Redis Cluster에서는
				// 자동 failover, Pod 재시작, 네트워크 분할 등으로 인해 replica(slave) 역할을 하고 있을 수 있습니다.
				// 슬롯을 이동하려면 해당 노드가 master 역할을 해야 하므로, 실제 역할을 확인합니다.
				leaderPod := k8smeta.GetPodName(cr.Name, "leader", int(leaderOrdinal))
				isLeader, err := cluster.IsLeaderNode(ctx, r.K8sClient, cr, leaderPod)
				if err != nil {
					logger.Error(err, "failed to check if pod is leader")
					return intctrlutil.RequeueE(ctx, err, "")
				}
				if !isLeader {
					// 실제 Redis에서 이 Pod가 replica 역할을 하고 있으므로, cluster failover를 수행하여
					// 해당 Pod를 master로 승격시킵니다. 이렇게 해야 해당 샤드의 슬롯을 다른 노드로 이동시킬 수 있습니다.
					logger.Info("Cluster Failover is initiated", "Shard.Index", leaderOrdinal, "Reason", "Pod is replica, promoting to master for slot migration")
					if err = cluster.ClusterFailover(ctx, r.K8sClient, cr, leaderPod); err != nil {
						logger.Error(err, "Failed to initiate cluster failover")
						return intctrlutil.RequeueE(ctx, err, "")
					}
				}

				// Step 1: 해당 샤드에 연결된 모든 Follower 노드를 클러스터에서 제거합니다.
				// Follower를 먼저 제거한 후 Leader를 제거해야 안전합니다.
				if err = cluster.RemoveRedisFollowerNodesFromCluster(ctx, r.K8sClient, cr, leaderOrdinal); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to remove follower nodes before downscale")
				}

				// Step 2: 해당 샤드의 슬롯을 다른 노드로 리샤딩합니다.
				// Round-robin 방식으로 대상 노드를 선택합니다:
				//   - shardIdx % leaderReplicas를 사용하여 나머지 노드에 고르게 분산
				//   - 예: 5개에서 3개로 줄일 때, 샤드 4의 슬롯은 노드 1(4%3=1)로 이동
				shardMoveNodeIdx := leaderOrdinal % desiredLeaderReplicas
				if err = cluster.ReshardRedisCluster(ctx, r.K8sClient, cr, leaderOrdinal, shardMoveNodeIdx); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to reshard cluster during downscale")
				}

				// Step 3: 해당 샤드의 노드를 클러스터에서 제거합니다.
				leaderPodNodeID, err := cluster.GetNodeIDByPod(ctx, r.K8sClient, cr, leaderPod)
				if err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to resolve source node id for downscale removal")
				}
				if err = cluster.RemoveRedisNodeByID(ctx, r.K8sClient, cr, leaderPodNodeID); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to remove source node after reshard")
				}
			}

			// Step 4: 클러스터를 재밸런싱합니다.
			// 리샤딩 후에도 슬롯이 불균등하게 분배될 수 있으므로, 전체적으로 재밸런싱합니다.
			logger.Info("Redis cluster is downscaled... Rebalancing the cluster")
			if err = cluster.RebalanceRedisCluster(ctx, r.K8sClient, cr); err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to rebalance cluster after downscale")
			}
			logger.Info("Redis cluster is downscaled... Rebalancing the cluster is done")

			// Step 5: 제거된 ordinal의 PVC 삭제
			// 다운스케일 후 남아있는 PVC(node-conf, data-persistence)를 삭제합니다.
			// PVC에 이전 클러스터 정보(nodes.conf)가 남아있으면 스케일업 시 "Node is not empty" 에러가 발생합니다.
			for ordinal := lastLeaderOrdinal; ordinal >= firstOrdinalToRemove; ordinal-- {
				if err := r.deleteOrphanedPVCs(ctx, cr, int(ordinal)); err != nil {
					logger.Error(err, "failed to delete orphaned PVCs", "ordinal", ordinal)
				}
			}

			// 다운스케일 작업이 완료되었으므로 10초 후에 다시 reconcile합니다.
			return intctrlutil.RequeueAfter(ctx, time.Second*10, "")
		} else {
			// 실제 클러스터의 노드 수와 StatefulSet의 리플리카 수가 일치하지 않으면
			// 다운스케일을 건너뜁니다. 먼저 클러스터 상태를 정상화해야 합니다.
			logger.Info("masterCount is not equal to leader statefulset replicas, skip downscale",
				"masterCount", realRedisNodeCount,
				"statefulSetLeaderReplicas", currentLeaderReplicas,
				"desiredLeaderReplicas", desiredLeaderReplicas)
		}
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

	err = cluster.CreateRedisLeaderSTS(ctx, cr, r.K8sClient)
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to create redis leader")
	}

	if desiredLeaderReplicas > 0 {
		err = cluster.CreateRedisLeaderService(ctx, cr, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to create redis follower")
		}
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

		err = cluster.CreateRedisFollowerSTS(ctx, cr, r.K8sClient)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "")
		}

		if desiredFollwerReplicas != 0 {
			err = cluster.CreateRedisFollowerService(ctx, cr, r.K8sClient)
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
	nc, err := cluster.GetClusterAllNodeCount(ctx, r.K8sClient, cr, "")
	if err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to get cluster node count")
	}
	if nc != desiredTotalReplicas {
		logger.Info("Creating redis cluster by executing cluster creation commands")
		// 실제로 레디스 클러스터내에서 마스터 역할을 하는 개수
		currentLeaderCount, err := cluster.GetClusterMasterNodeCount(ctx, r.K8sClient, cr)
		if err != nil {
			return intctrlutil.RequeueE(ctx, err, "failed to get cluster master node count")
		}
		// TODO: currentLeaderCount > desiredLeaderReplica인경우도 처리해야할까?
		if currentLeaderCount != desiredLeaderReplicas {
			logger.Info("Not all leader are part of the cluster...", "Leaders.Count", currentLeaderCount, "Instance.Size", desiredLeaderReplicas)
			if currentLeaderCount <= 1 {
				// 클러스터 미형성 상태: leader-0만 자기 자신을 master로 인식 (cluster_known_nodes == 1)
				// redis-cli --cluster create로 모든 leader를 포함한 새 클러스터를 생성합니다.
				result, err := cluster.CreateRedisCluster(ctx, r.K8sClient, cr)
				if err != nil {
					logger.Error(err, "failed to create redis cluster")
					return intctrlutil.RequeueE(ctx, err, "failed to create redis cluster")
				}
				logger.Info("Redis cluster creation result", "Result", result)
			} else if currentLeaderCount < desiredLeaderReplicas {
				// 클러스터가 이미 형성된 상태에서 스케일 업: 새 leader 노드를 기존 클러스터에 추가합니다.
				// redis-cli --cluster add-node로 개별 노드를 추가한 후 슬롯을 재배분합니다.
				if err := cluster.AddRedisLeaderNodeToCluster(ctx, r.K8sClient, cr); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to add leader node to cluster")
				}
				if err := cluster.RebalanceRedisClusterEmptyMasters(ctx, r.K8sClient, cr); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to rebalance empty masters")
				}
			}
		} else {
			currentFollowerCount, err := cluster.GetClusterFollowerNodeCount(ctx, r.K8sClient, cr)
			if err != nil {
				return intctrlutil.RequeueE(ctx, err, "failed to get cluster follower node count")
			}
			if desiredFollwerReplicas == 0 {
				logger.Info("no follower/replicas configured, skipping replication configuration", "Leaders.Count", currentLeaderCount, "Leader.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
			} else if currentFollowerCount < desiredFollwerReplicas {
				logger.Info("All leader are part of the cluster, adding follower/replicas", "Leaders.Count", currentLeaderCount, "Instance.Size", desiredLeaderReplicas, "Follower.Replicas", desiredFollwerReplicas)
				if err := cluster.ExecuteRedisReplicationCommand(ctx, r.K8sClient, cr); err != nil {
					return intctrlutil.RequeueE(ctx, err, "failed to execute replication command")
				}
			} else if currentFollowerCount > desiredFollwerReplicas {
				logger.Info("follower count is higher than desired; waiting for node cleanup", "Current.Follower.Count", currentFollowerCount, "Desired.Follower.Count", desiredFollwerReplicas)
			} else {
				logger.Info("leader/follower counts match desired, waiting for total cluster node convergence", "Current.Leader.Count", currentLeaderCount, "Desired.Leader.Count", desiredLeaderReplicas, "Current.Follower.Count", currentFollowerCount, "Desired.Follower.Count", desiredFollwerReplicas)
			}

			return intctrlutil.RequeueAfter(ctx, time.Second*60,
				"Redis cluster count is not desired", "\n",
				"Current.Count", nc,
				"Desired.Count", desiredTotalReplicas, "\n",
				"Current.Leader.Count", currentLeaderCount,
				"Current.Follower.Count", currentFollowerCount, "\n",
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
	if ncCheck, err := cluster.GetClusterAllNodeCount(ctx, r.K8sClient, cr, ""); err != nil {
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
