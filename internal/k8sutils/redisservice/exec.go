package redisservice

import (
	"bytes"
	"context"
	"fmt"
	"strings"

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

// extractLastMeaningfulLine은 redis-cli 출력에서 마지막 비어있지 않은 줄을 추출합니다.
func extractLastMeaningfulLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return output
}

// ExecuteCommandInPod는 Pod에서 명령을 실행합니다.
func ExecuteCommandInPod(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, cmd []string, podName string) {
	execOut, execErr := executeCommand(ctx, client, cr, cmd, podName)
	if execErr != nil {
		log.FromContext(ctx).Error(execErr, "Could not execute command", "Command", cmd, "Result", extractLastMeaningfulLine(execOut))
		log.FromContext(ctx).V(1).Info("Command full output", "Output", execOut)
		return
	}
	log.FromContext(ctx).V(1).Info("Successfully executed the command", "Command", cmd, "Result", extractLastMeaningfulLine(execOut))
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
		return "", err
	}

	// Pod 정보 가져오기
	pod, err := client.CoreV1().Pods(cr.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
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

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", execURL)
	if err != nil {
		return "", err
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	if err != nil {
		out := execOut.String()
		errOut := execErr.String()
		return out, fmt.Errorf(
			"execute command failed: %w; stdout: %s; stderr: %s",
			err, out, errOut,
		)
	}
	return execOut.String(), nil
}
