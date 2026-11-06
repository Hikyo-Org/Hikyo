package securitydeployment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// podIO reaches into Pods through the Kubernetes API, so no kubectl is installed.
// The fake clientset serves neither exec nor a chosen log body, hence the seam.
type podIO interface {
	Exec(ctx context.Context, pod, container string, command []string) ([]byte, error)
	Logs(ctx context.Context, pod, container string) ([]byte, error)
}

type clusterPods struct {
	config    *rest.Config
	client    kubernetes.Interface
	namespace string
}

func (c clusterPods) Exec(ctx context.Context, pod, container string, command []string) ([]byte, error) {
	request := c.client.CoreV1().RESTClient().Post().Namespace(c.namespace).Resource("pods").Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{Container: container, Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	spdy, err := remotecommand.NewSPDYExecutor(c.config, http.MethodPost, request.URL())
	if err != nil {
		return nil, err
	}
	websocket, err := remotecommand.NewWebSocketExecutor(c.config, http.MethodGet, request.URL().String())
	if err != nil {
		return nil, err
	}
	executor, err := remotecommand.NewFallbackExecutor(websocket, spdy, func(err error) bool { return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err) })
	if err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	out, stderr := &boundedOutput{cancel: cancel}, &boundedOutput{cancel: cancel}
	err = executor.StreamWithContext(child, remotecommand.StreamOptions{Stdout: out, Stderr: stderr})
	if out.exceeded || stderr.exceeded {
		return nil, errors.New("security deployment: command output exceeds limit")
	}
	if err != nil {
		// Stdout carries structured workload data and is never returned. Stderr reaches
		// the journal, so only its scrubbed final lines do.
		return nil, fmt.Errorf("security deployment: exec in Pod %s failed (%w)%s", pod, err, stderrTail(stderr.buffer.Bytes(), nil))
	}
	return append([]byte(nil), out.buffer.Bytes()...), nil
}

func (c clusterPods) Logs(ctx context.Context, pod, container string) ([]byte, error) {
	limit := int64(maxRecordBytes)
	stream, err := c.client.CoreV1().Pods(c.namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, LimitBytes: &limit}).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, io.LimitReader(stream, limit+1)); err != nil {
		return nil, err
	}
	if out.Len() > maxRecordBytes {
		return nil, errors.New("security deployment: command output exceeds limit")
	}
	return out.Bytes(), nil
}
