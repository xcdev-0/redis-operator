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
	"github.com/xcdev-0/redis-operator/internal/k8sutils/clustermembership"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/clusterresource"
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
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// RedisClusterReconciler reconciles a RedisCluster object
type RedisClusterReconciler struct {
	client.Client
	*statefulset.StatefulSetService
	K8sClient kubernetes.Interface
	Recorder  record.EventRecorder
}

type replicaPlan struct {
	desiredLeaderReplicas   int32
	desiredFollowerReplicas int32
	desiredTotalReplicas    int32
	currentLeaderReplicas   int32
	currentFollowerReplicas int32
}

// +kubebuilder:rbac:groups=ejlabs.in,resources=redisclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ejlabs.in,resources=redisclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cr := &rcvb2.RedisCluster{}
	err := r.Get(ctx, req.NamespacedName, cr)
	if err != nil {
		return intctrlutil.RequeueECheck(ctx, err, "failed to get redis cluster instance")
	}

	if cr.GetDeletionTimestamp() != nil {
		return intctrlutil.Reconciled()
	}
	if shouldSkipReconcile(ctx, cr) {
		return intctrlutil.Reconciled()
	}

	cr.SetDefault()
	plan := r.buildReplicaPlan(ctx, cr)

	if result, handled, err := r.reconcileLeaderDownscale(ctx, cr, plan); err != nil || handled {
		return result, err
	}
	if result, handled, err := r.reconcileStatefulResources(ctx, cr, plan); err != nil || handled {
		return result, err
	}
	if result, handled, err := r.reconcileBootstrapStatus(ctx, cr, plan); err != nil || handled {
		return result, err
	}
	if result, handled, err := r.reconcileClusterMembership(ctx, cr, plan); err != nil || handled {
		return result, err
	}
	if result, handled, err := r.reconcileClusterHealth(ctx, cr, plan); err != nil || handled {
		return result, err
	}

	if err := clusterresource.UpdateRedisRoleLabels(ctx, r.K8sClient, cr); err != nil {
		return intctrlutil.RequeueE(ctx, err, "failed to update redis role labels")
	}

	return intctrlutil.RequeueAfter(ctx, time.Second*10, "")
}

func (r *RedisClusterReconciler) buildReplicaPlan(ctx context.Context, cr *rcvb2.RedisCluster) replicaPlan {
	desiredLeaderReplicas := cr.Spec.GetReplicaCount("leader")
	desiredFollowerReplicas := cr.Spec.GetReplicaCount("follower")

	return replicaPlan{
		desiredLeaderReplicas:   desiredLeaderReplicas,
		desiredFollowerReplicas: desiredFollowerReplicas,
		desiredTotalReplicas:    desiredLeaderReplicas + desiredFollowerReplicas,
		currentLeaderReplicas:   r.GetStatefulSetReplicas(ctx, cr.Namespace, k8smeta.GetStatefulSetName(cr.Name, "leader")),
		currentFollowerReplicas: r.GetStatefulSetReplicas(ctx, cr.Namespace, k8smeta.GetStatefulSetName(cr.Name, "follower")),
	}
}

func (r *RedisClusterReconciler) reconcileLeaderDownscale(ctx context.Context, cr *rcvb2.RedisCluster, plan replicaPlan) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)
	leaderSTSName := k8smeta.GetStatefulSetName(cr.Name, "leader")
	followerSTSName := k8smeta.GetStatefulSetName(cr.Name, "follower")

	if plan.desiredLeaderReplicas >= plan.currentLeaderReplicas {
		return ctrl.Result{}, false, nil
	}
	if !r.IsStatefulSetReady(ctx, cr.Namespace, leaderSTSName) || !r.IsStatefulSetReady(ctx, cr.Namespace, followerSTSName) {
		return ctrl.Result{}, true, nil
	}
	if plan.desiredLeaderReplicas <= 0 {
		result, err := intctrlutil.RequeueE(ctx, fmt.Errorf("desired leader replicas must be greater than zero for downscale"), "invalid downscale target")
		return result, true, err
	}

	stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "leader downscale cleanup")
	if err != nil || !stable {
		return result, true, err
	}

	lastOrdinalToRemove := plan.currentLeaderReplicas - 1
	firstOrdinalToRemove := plan.desiredLeaderReplicas
	for ordinalToRemove := lastOrdinalToRemove; ordinalToRemove >= firstOrdinalToRemove; ordinalToRemove-- {
		leaderPod := k8smeta.GetPodName(cr.Name, "leader", int(ordinalToRemove))

		sourceMasterNodeID, err := clustermembership.GetMasterNodeIDByPod(ctx, r.K8sClient, cr, leaderPod)
		if err != nil {
			logger.Info("failed to resolve source master from overflow leader pod; skipping ordinal",
				"Ordinal", ordinalToRemove,
				"Leader.Pod", leaderPod,
				"Error", err.Error())
			continue
		}
		sourceMasterJoined, err := clustermembership.IsNodeIDInCluster(ctx, r.K8sClient, cr, sourceMasterNodeID)
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to check source master node id cluster membership for overflow ordinal")
			return result, true, requeueErr
		}
		if !sourceMasterJoined {
			logger.Info("source master node id already absent in cluster membership; skipping ordinal",
				"Ordinal", ordinalToRemove,
				"Leader.Pod", leaderPod,
				"Source.Master.NodeID", sourceMasterNodeID)
			continue
		}

		transferMasterNodeID, hasTransferMaster, err := r.pickTransferMasterNodeID(ctx, cr, sourceMasterNodeID, plan.desiredLeaderReplicas, ordinalToRemove)
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to pick transfer master node id during downscale")
			return result, true, requeueErr
		}
		if !hasTransferMaster {
			logger.Info("no transfer master candidate found yet for overflow source; waiting",
				"Ordinal", ordinalToRemove,
				"Source.Master.NodeID", sourceMasterNodeID,
				"Desired.Leader.Replicas", plan.desiredLeaderReplicas)
			result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for transfer master candidate during downscale")
			return result, true, requeueErr
		}

		attachedFollowerNodeIDs, err := clustermembership.GetFollowerNodeIDsByMasterNodeID(ctx, r.K8sClient, cr, sourceMasterNodeID)
		logger.Info("attached follower node ids", "Attached.Follower.NodeIDs", attachedFollowerNodeIDs)
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get follower node ids by source master node id")
			return result, true, requeueErr
		}
		for _, followerNodeID := range attachedFollowerNodeIDs {
			if err := clustermembership.RemoveRedisNodeByID(ctx, r.K8sClient, cr, followerNodeID); err != nil {
				result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to remove attached follower node during downscale")
				return result, true, requeueErr
			}
		}

		if err := clustermembership.ReshardRedisClusterByNodeID(ctx, r.K8sClient, cr, sourceMasterNodeID, transferMasterNodeID); err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to reshard cluster during leader downscale")
			return result, true, requeueErr
		}
		if err := clustermembership.RemoveRedisNodeByID(ctx, r.K8sClient, cr, sourceMasterNodeID); err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to remove source master node during downscale")
			return result, true, requeueErr
		}
		if err := clustermembership.RebalanceRedisCluster(ctx, r.K8sClient, cr); err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to rebalance cluster after downscale node removal")
			return result, true, requeueErr
		}

		logger.Info("completed one overflow leader ordinal cleanup pass",
			"Ordinal", ordinalToRemove,
			"Source.Master.NodeID", sourceMasterNodeID,
			"Transfer.Master.NodeID", transferMasterNodeID,
			"Removed.Attached.Follower.Count", len(attachedFollowerNodeIDs))
		result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*10, "processed one overflow leader ordinal cleanup")
		return result, true, requeueErr
	}

	logger.Info("overflow cluster membership cleanup pass completed; proceeding to statefulset reconcile",
		"Current.Leader.Replicas", plan.currentLeaderReplicas,
		"Desired.Leader.Replicas", plan.desiredLeaderReplicas)
	return ctrl.Result{}, false, nil
}

func (r *RedisClusterReconciler) reconcileStatefulResources(ctx context.Context, cr *rcvb2.RedisCluster, plan replicaPlan) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)
	leaderSTSName := k8smeta.GetStatefulSetName(cr.Name, "leader")
	followerSTSName := k8smeta.GetStatefulSetName(cr.Name, "follower")

	if (cr.Status.ReadyLeaderReplicas == 0 && cr.Status.ReadyFollowerReplicas == 0) ||
		cr.Status.ReadyLeaderReplicas != plan.desiredLeaderReplicas {
		result, handled, err := r.ensureStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterInitializing,
			Reason:                rcvb2.InitializingClusterLeaderReason,
			ReadyLeaderReplicas:   cr.Status.ReadyLeaderReplicas,
			ReadyFollowerReplicas: cr.Status.ReadyFollowerReplicas,
		}, "failed to update status")
		if err != nil || handled {
			return result, true, err
		}
	}

	if plan.desiredLeaderReplicas > 0 {
		if err := clusterresource.CreateRedisLeaderService(ctx, cr, r.K8sClient); err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to create redis leader service")
			return result, true, requeueErr
		}
	}
	if plan.desiredLeaderReplicas < plan.currentLeaderReplicas {
		firstOrdinalToRemove := plan.desiredLeaderReplicas
		lastLeaderOrdinal := plan.currentLeaderReplicas - 1
		downscaleCleanupDone, err := r.isRoleDownscaleClusterCleanupComplete(ctx, cr, firstOrdinalToRemove, lastLeaderOrdinal, "leader", "follower")
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to verify downscale cleanup completion before statefulset reconcile")
			return result, true, requeueErr
		}
		if !downscaleCleanupDone {
			logger.Info("downscale cleanup is not completed yet; delaying StatefulSet replica reconcile",
				"Current.Leader.Replicas", plan.currentLeaderReplicas,
				"Current.Follower.Replicas", plan.currentFollowerReplicas,
				"Desired.Leader.Replicas", plan.desiredLeaderReplicas,
				"Desired.Follower.Replicas", plan.desiredFollowerReplicas)
			result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for downscale cleanup completion before sts reconcile")
			return result, true, requeueErr
		}
	}
	if err := clusterresource.CreateRedisLeaderSTS(ctx, cr, r.K8sClient); err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to create redis leader")
		return result, true, requeueErr
	}
	if !r.IsStatefulSetReady(ctx, cr.Namespace, leaderSTSName) {
		return ctrl.Result{}, true, nil
	}

	if (cr.Status.ReadyLeaderReplicas == 0 && cr.Status.ReadyFollowerReplicas == 0) ||
		cr.Status.ReadyFollowerReplicas != plan.desiredFollowerReplicas {
		result, handled, err := r.ensureStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterInitializing,
			Reason:                rcvb2.InitializingClusterFollowerReason,
			ReadyLeaderReplicas:   plan.desiredLeaderReplicas,
			ReadyFollowerReplicas: cr.Status.ReadyFollowerReplicas,
		}, "failed to update status")
		if err != nil || handled {
			return result, true, err
		}
	}

	if plan.desiredFollowerReplicas != 0 {
		if err := clusterresource.CreateRedisFollowerService(ctx, cr, r.K8sClient); err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to create redis follower service")
			return result, true, requeueErr
		}
	}
	if plan.desiredFollowerReplicas < plan.currentFollowerReplicas {
		firstOrdinalToRemove := plan.desiredFollowerReplicas
		lastFollowerOrdinal := plan.currentFollowerReplicas - 1
		followerDownscaleCleanupDone, err := r.isRoleDownscaleClusterCleanupComplete(ctx, cr, firstOrdinalToRemove, lastFollowerOrdinal, "follower")
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to verify follower downscale cleanup completion before statefulset reconcile")
			return result, true, requeueErr
		}
		if !followerDownscaleCleanupDone {
			logger.Info("follower downscale cleanup is not completed yet; delaying follower StatefulSet replica reconcile",
				"Current.Follower.Replicas", plan.currentFollowerReplicas,
				"Desired.Follower.Replicas", plan.desiredFollowerReplicas)
			result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*10, "waiting for follower downscale cleanup completion before follower sts reconcile")
			return result, true, requeueErr
		}
	}
	if err := clusterresource.CreateRedisFollowerSTS(ctx, cr, r.K8sClient); err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to create redis follower statefulset")
		return result, true, requeueErr
	}
	if !r.IsStatefulSetReady(ctx, cr.Namespace, followerSTSName) {
		return ctrl.Result{}, true, nil
	}

	return ctrl.Result{}, false, nil
}

func (r *RedisClusterReconciler) reconcileBootstrapStatus(ctx context.Context, cr *rcvb2.RedisCluster, plan replicaPlan) (ctrl.Result, bool, error) {
	if cr.Status.ReadyLeaderReplicas == plan.desiredLeaderReplicas &&
		cr.Status.ReadyFollowerReplicas == plan.desiredFollowerReplicas {
		return ctrl.Result{}, false, nil
	}

	return r.ensureStatus(ctx, cr, rcvb2.RedisClusterStatus{
		State:                 rcvb2.RedisClusterBootstrap,
		Reason:                rcvb2.BootstrapClusterReason,
		ReadyLeaderReplicas:   plan.desiredLeaderReplicas,
		ReadyFollowerReplicas: plan.desiredFollowerReplicas,
	}, "failed to update bootstrap status")
}

func (r *RedisClusterReconciler) reconcileClusterMembership(ctx context.Context, cr *rcvb2.RedisCluster, plan replicaPlan) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)

	nodeCount, err := clustermembership.GetClusterNodeCount(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get cluster node count")
		return result, true, requeueErr
	}
	if nodeCount == plan.desiredTotalReplicas {
		return ctrl.Result{}, false, nil
	}

	logger.Info("Creating redis cluster by executing cluster creation commands")
	leaderNodeCount, err := clustermembership.GetClusterMasterNodeCount(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get cluster master node count")
		return result, true, requeueErr
	}
	joinedLeaderPodCount, err := r.getJoinedRolePodCount(ctx, cr, "leader", plan.desiredLeaderReplicas)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get joined leader pod count")
		return result, true, requeueErr
	}
	needLeaderJoin := joinedLeaderPodCount < plan.desiredLeaderReplicas

	if leaderNodeCount != plan.desiredLeaderReplicas || needLeaderJoin {
		logger.Info("Not all leader are part of the cluster...",
			"Leaders.Count", leaderNodeCount,
			"Joined.Leader.Pods", joinedLeaderPodCount,
			"Instance.Size", plan.desiredLeaderReplicas)
		if leaderNodeCount <= 1 && joinedLeaderPodCount <= 1 {
			result, err := clustermembership.CreateRedisCluster(ctx, r.K8sClient, cr)
			if err != nil {
				logger.Error(err, "failed to create redis cluster")
				requeueResult, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to create redis cluster")
				return requeueResult, true, requeueErr
			}
			logger.Info("Redis cluster creation result", "Result", result)
		} else if needLeaderJoin {
			stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "leader add")
			if err != nil || !stable {
				return result, true, err
			}

			if err := clustermembership.AddRedisLeaderNodeToCluster(ctx, r.K8sClient, cr, plan.desiredLeaderReplicas); err != nil {
				requeueResult, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to add leader node to cluster")
				return requeueResult, true, requeueErr
			}
			if err := clustermembership.RebalanceRedisClusterEmptyMasters(ctx, r.K8sClient, cr); err != nil {
				requeueResult, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to rebalance empty masters")
				return requeueResult, true, requeueErr
			}
		} else {
			logger.Info("leader master count mismatch without unjoined leader pods; waiting for convergence",
				"Leaders.Count", leaderNodeCount,
				"Joined.Leader.Pods", joinedLeaderPodCount,
				"Instance.Size", plan.desiredLeaderReplicas)
		}
		return ctrl.Result{}, false, nil
	}

	stable, result, err := r.ensureClusterStableForMembershipChange(ctx, cr, "follower replication")
	if err != nil || !stable {
		return result, true, err
	}

	currentFollowerCount, err := clustermembership.GetClusterFollowerNodeCount(ctx, r.K8sClient, cr)
	if err != nil {
		requeueResult, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get cluster follower node count")
		return requeueResult, true, requeueErr
	}
	if plan.desiredFollowerReplicas == 0 {
		logger.Info("no follower/replicas configured, skipping replication configuration",
			"Leaders.Count", leaderNodeCount,
			"Leader.Size", plan.desiredLeaderReplicas,
			"Follower.Replicas", plan.desiredFollowerReplicas)
	} else if currentFollowerCount < plan.desiredFollowerReplicas {
		logger.Info("All leader are part of the cluster, adding follower/replicas",
			"Leaders.Count", leaderNodeCount,
			"Instance.Size", plan.desiredLeaderReplicas,
			"Follower.Replicas", plan.desiredFollowerReplicas)
		if err := clustermembership.ExecuteRedisReplicationCommand(ctx, r.K8sClient, cr, plan.desiredFollowerReplicas, plan.desiredLeaderReplicas); err != nil {
			requeueResult, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to execute replication command")
			return requeueResult, true, requeueErr
		}
	} else if currentFollowerCount > plan.desiredFollowerReplicas {
		logger.Info("follower count is higher than desired; waiting for node cleanup",
			"Current.Follower.Count", currentFollowerCount,
			"Desired.Follower.Count", plan.desiredFollowerReplicas)
	} else {
		logger.Info("leader/follower counts match desired, waiting for total cluster node convergence",
			"Current.Leader.Count", leaderNodeCount,
			"Desired.Leader.Count", plan.desiredLeaderReplicas,
			"Current.Follower.Count", currentFollowerCount,
			"Desired.Follower.Count", plan.desiredFollowerReplicas)
	}

	result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*60,
		"Redis cluster count is not desired",
		"Current.Count", nodeCount,
		"Desired.Count", plan.desiredTotalReplicas,
		"Current.Leader.Count", leaderNodeCount,
		"Current.Follower.Count", currentFollowerCount,
		"Desired.Leader.Count", plan.desiredLeaderReplicas,
		"Desired.Follower.Count", plan.desiredFollowerReplicas)
	return result, true, requeueErr
}

func (r *RedisClusterReconciler) reconcileClusterHealth(ctx context.Context, cr *rcvb2.RedisCluster, plan replicaPlan) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)

	logger.Info("Number of Redis nodes match desired")
	unhealthyNodeCount, err := clustermembership.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to determine unhealthy node count in cluster")
		return result, true, requeueErr
	}

	if int(plan.desiredTotalReplicas) > 1 && unhealthyNodeCount > 0 {
		result, handled, err := r.ensureStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterFailed,
			Reason:                "RedisCluster has unhealthy nodes",
			ReadyLeaderReplicas:   plan.desiredLeaderReplicas,
			ReadyFollowerReplicas: plan.desiredFollowerReplicas,
		}, "failed to update failed cluster status")
		if err != nil || handled {
			return result, true, err
		}

		logger.Info("healthy leader count does not match desired; attempting to repair disconnected masters")
		if err = clustermembership.RepairDisconnectedNodes(ctx, r.K8sClient, cr); err != nil {
			logger.Error(err, "failed to repair disconnected masters")
		}

		err = retry.Do(func() error {
			nc, nErr := clustermembership.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
			if nErr != nil {
				return nErr
			}
			if nc == 0 {
				return nil
			}
			return fmt.Errorf("%d unhealthy nodes", nc)
		}, retry.Attempts(3), retry.Delay(time.Second*5))
		if err == nil {
			logger.Info("repairing unhealthy masters successful, no unhealthy masters left")
			result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*30, "no unhealthy nodes found after repairing disconnected masters")
			return result, true, requeueErr
		}

		unhealthyNodeCount, err = clustermembership.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
		if err != nil {
			result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to determine unhealthy node count in cluster")
			return result, true, requeueErr
		}
		if int(plan.desiredTotalReplicas) > 1 && int(unhealthyNodeCount) >= int(plan.desiredTotalReplicas)-1 {
			result, requeueErr := intctrlutil.RequeueE(ctx, fmt.Errorf("cluster broken: %d/%d nodes unhealthy, manual intervention required", unhealthyNodeCount, plan.desiredTotalReplicas), "")
			return result, true, requeueErr
		}
	}

	if ncCheck, err := clustermembership.GetClusterNodeCount(ctx, r.K8sClient, cr); err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get cluster node count")
		return result, true, requeueErr
	} else if ncCheck == plan.desiredTotalReplicas {
		clustermembership.RebalanceIfEmptyMasterExists(ctx, r.K8sClient, cr)
	}

	if cr.Status.ReadyLeaderReplicas == plan.desiredLeaderReplicas &&
		cr.Status.ReadyFollowerReplicas == plan.desiredFollowerReplicas &&
		cr.Status.State != rcvb2.RedisClusterReady &&
		cr.Status.State != rcvb2.RedisClusterInitializing &&
		clustermembership.RedisClusterStatusHealth(ctx, r.K8sClient, cr) {
		return r.ensureStatus(ctx, cr, rcvb2.RedisClusterStatus{
			State:                 rcvb2.RedisClusterReady,
			Reason:                rcvb2.ReadyClusterReason,
			ReadyLeaderReplicas:   plan.desiredLeaderReplicas,
			ReadyFollowerReplicas: plan.desiredFollowerReplicas,
		}, "failed to update ready cluster status")
	}

	return ctrl.Result{}, false, nil
}

func (r *RedisClusterReconciler) ensureStatus(ctx context.Context, cr *rcvb2.RedisCluster, status rcvb2.RedisClusterStatus, failureMessage string) (ctrl.Result, bool, error) {
	requeue, err := r.updateStatus(ctx, cr, status)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, failureMessage)
		return result, true, requeueErr
	}
	if requeue {
		result, requeueErr := intctrlutil.Requeue()
		return result, true, requeueErr
	}
	return ctrl.Result{}, false, nil
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

		joined, err := clustermembership.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
		if err != nil {
			return "", false, fmt.Errorf("failed to check cluster membership for transfer pod %s: %w", podName, err)
		}
		if !joined {
			continue
		}

		masterNodeID, err := clustermembership.GetMasterNodeIDByPod(ctx, r.K8sClient, cr, podName)
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

	unhealthyNodeCount, err := clustermembership.UnhealthyNodesInCluster(ctx, r.K8sClient, cr)
	if err != nil {
		result, requeueErr := intctrlutil.RequeueE(ctx, err, "failed to get unhealthy node count before membership change", "Action", action)
		return false, result, requeueErr
	}
	if unhealthyNodeCount > 0 {
		logger.Info("cluster has unhealthy nodes, delaying membership change",
			"Action", action,
			"Unhealthy.Node.Count", unhealthyNodeCount)
		if repairErr := clustermembership.RepairDisconnectedNodes(ctx, r.K8sClient, cr); repairErr != nil {
			logger.Error(repairErr, "failed to repair disconnected nodes before membership change", "Action", action)
		}
		result, requeueErr := intctrlutil.RequeueAfter(ctx, time.Second*30, "cluster has unhealthy nodes, waiting before membership change", "Action", action)
		return false, result, requeueErr
	}

	pendingNodeCount, err := clustermembership.GetClusterPendingNodeCount(ctx, r.K8sClient, cr)
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
		joined, err := clustermembership.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
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
			joined, err := clustermembership.IsPodJoinedCluster(ctx, r.K8sClient, cr, podName)
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
