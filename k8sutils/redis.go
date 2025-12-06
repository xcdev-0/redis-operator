package k8sutils

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/remotecommand"

	ctrl "sigs.k8s.io/controller-runtime"
)

func RunRedisCLI(k8scl kubernetes.Interface, namespace string, podName string, cmd []string) (string, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get Kubernetes client config: %w", err)
	}

	req := k8scl.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: "redis",
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var exeOut, exeErr bytes.Buffer
	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdout: &exeOut,
		Stderr: &exeErr,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %w, stdout: %s, stderr: %s", err, exeOut.String(), exeErr.String())
	}

	return exeOut.String(), nil
}
