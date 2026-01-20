package redisservice

import (
	"bytes"
	"context"
	"fmt"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/client"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ExecuteCommandInPod는 Pod에서 명령을 실행합니다.
func ExecuteCommandInPod(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, cmd []string, podName string) {
	execOut, execErr := executeCommand(ctx, client, cr, cmd, podName)
	if execErr != nil {
		log.FromContext(ctx).Error(execErr, "Could not execute command", "Command", cmd, "Output", execOut)
		return
	}
	log.FromContext(ctx).V(1).Info("Successfully executed the command", "Command", cmd, "Output", execOut)
}

// ExecuteCommandInPodWithResult는 Pod에서 명령을 실행하고 결과와 에러를 반환합니다.
func ExecuteCommandInPodWithResult(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, cmd []string, podName string) (stdout string, err error) {
	return executeCommand(ctx, client, cr, cmd, podName)
}

// executeCommand는 Pod에서 명령을 실행하고 결과를 반환합니다.
func executeCommand(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, cmd []string, podName string) (stdout string, stderr error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)
	config, err := k8smeta.GenerateK8sConfig()()
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not find pod to execute")
		return "", err
	}

	// Pod 정보 가져오기
	pod, err := client.CoreV1().Pods(cr.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not find pod to execute")
		return "", err
	}

	// redis 컨테이너 찾기
	targetContainer := -1
	for i, container := range pod.Spec.Containers {
		if container.Name == consts.MainContainerName {
			targetContainer = i
			break
		}
	}
	if targetContainer < 0 {
		err := fmt.Errorf("redis container not found in pod %s", podName)
		log.FromContext(ctx).Error(err, "Could not find redis container")
		return "", err
	}

	req := client.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace(cr.Namespace).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: pod.Spec.Containers[targetContainer].Name,
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	// 디버깅: URL 확인
	execURL := req.URL()
	log.FromContext(ctx).Info("Exec request URL", "url", execURL.String())

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", execURL)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to init executor")
		return "", err
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	if err != nil {
		return execOut.String(), fmt.Errorf("execute command with error: %w, stderr: %s", err, execErr.String())
	}
	return execOut.String(), nil
}
