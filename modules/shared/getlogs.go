package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	// Default kubeconfig path
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")

	// Hard-coded namespace and pod name
	namespace := "default"
	podName := "echoserver-6c88d85f75-4hswg"
	container := ""

	// Build Kubernetes client config
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		// Fallback to in-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building kubeconfig: %v\n", err)
			os.Exit(1)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Prepare log request
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,
		Timestamps: true,
		SinceTime:  &metav1.Time{Time: time.Time{}},
		//TailLines:  ptr.To[int64](0),
	})

	// Stream logs
	stream, err := req.Stream(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log stream: %v\n", err)
		os.Exit(1)
	}
	defer stream.Close()

	// Copy logs to stdout
	if _, err := io.Copy(os.Stdout, stream); err != nil {
		fmt.Fprintf(os.Stderr, "Error copying logs to stdout: %v\n", err)
		os.Exit(1)
	}
}
