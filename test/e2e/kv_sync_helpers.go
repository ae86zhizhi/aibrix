/*
Copyright 2024 The Aibrix Team.

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

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/stretchr/testify/assert"
)

const (
	kvEventsTestNamespace = "kv-sync-test"
	testModelName         = "test-model"
	kvEventsPort          = 5557
	kvReplayPort          = 5558
)

// KVEventTestHelper provides utilities for KV event E2E testing
type KVEventTestHelper struct {
	k8sClient   *kubernetes.Clientset
	namespace   string
	modelName   string
	deployments []*appsv1.Deployment
}

// NewKVEventTestHelper creates a new helper instance
func NewKVEventTestHelper(client *kubernetes.Clientset, namespace string) *KVEventTestHelper {
	return &KVEventTestHelper{
		k8sClient: client,
		namespace: namespace,
		modelName: testModelName,
	}
}

// GetNamespace returns the namespace of the helper
func (h *KVEventTestHelper) GetNamespace() string {
	return h.namespace
}

// CreateTestNamespace creates a test namespace
func (h *KVEventTestHelper) CreateTestNamespace(t *testing.T) {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: h.namespace,
		},
	}
	_, err := h.k8sClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	if err != nil {
		t.Logf("Failed to create namespace %s: %v", h.namespace, err)
	}
}

// CleanupTestNamespace deletes the test namespace
func (h *KVEventTestHelper) CleanupTestNamespace(t *testing.T) {
	err := h.k8sClient.CoreV1().Namespaces().Delete(context.TODO(), h.namespace, metav1.DeleteOptions{})
	if err != nil {
		t.Logf("Failed to delete namespace %s: %v", h.namespace, err)
	}
}

// CreateVLLMPodWithKVEvents creates a vLLM pod with KV events enabled
func (h *KVEventTestHelper) CreateVLLMPodWithKVEvents(t *testing.T, name string, replicas int32) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: h.namespace,
			Labels: map[string]string{
				"model.aibrix.ai/name":            h.modelName,
				"model.aibrix.ai/port":            "8000",
				"model.aibrix.ai/kv-events-enabled": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"model.aibrix.ai/name": h.modelName,
					"app":                  name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name":            h.modelName,
						"model.aibrix.ai/kv-events-enabled": "true",
						"app":                              name,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "vllm-mock",
							Image: "aibrix/vllm-mock:nightly",
							// Use default CMD from Dockerfile instead of vLLM command
							// The mock app doesn't have vLLM installed
							Env: []v1.EnvVar{
								{
									Name: "DEPLOYMENT_NAME",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "metadata.labels['app']",
										},
									},
								},
								{
									Name: "POD_NAME",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: "MY_POD_IP",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
							Ports: []v1.ContainerPort{
								{Name: "api", ContainerPort: 8000, Protocol: v1.ProtocolTCP},
								{Name: "kv-events", ContainerPort: kvEventsPort, Protocol: v1.ProtocolTCP},
								{Name: "kv-replay", ContainerPort: kvReplayPort, Protocol: v1.ProtocolTCP},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("100m"),
									v1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Limits: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
							ReadinessProbe: &v1.Probe{
								ProbeHandler: v1.ProbeHandler{
									HTTPGet: &v1.HTTPGetAction{
										Path:   "/metrics",
										Port:   intstr.FromInt(8000),
										Scheme: v1.URISchemeHTTP,
									},
								},
								PeriodSeconds:    5,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								TimeoutSeconds:   1,
							},
						},
					},
				},
			},
		},
	}

	created, err := h.k8sClient.AppsV1().Deployments(h.namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	assert.NoError(t, err, "Failed to create deployment %s", name)
	
	h.deployments = append(h.deployments, created)
	return created
}

// WaitForDeploymentReady waits for a deployment to be ready
func (h *KVEventTestHelper) WaitForDeploymentReady(t *testing.T, name string, timeout time.Duration) {
	err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			deployment, err := h.k8sClient.AppsV1().Deployments(h.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			
			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
				t.Logf("Deployment %s is ready with %d replicas", name, deployment.Status.ReadyReplicas)
				return true, nil
			}
			
			t.Logf("Waiting for deployment %s to be ready. Ready: %d/%d", 
				name, deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
			return false, nil
		})
	
	assert.NoError(t, err, "Timeout waiting for deployment %s to be ready", name)
}

// GetPodsByDeployment returns pods for a deployment
func (h *KVEventTestHelper) GetPodsByDeployment(t *testing.T, deploymentName string) []v1.Pod {
	pods, err := h.k8sClient.CoreV1().Pods(h.namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	assert.NoError(t, err, "Failed to list pods for deployment %s", deploymentName)
	
	return pods.Items
}

// ValidateKVEventConnection validates that a pod can accept KV event connections
func (h *KVEventTestHelper) ValidateKVEventConnection(t *testing.T, podIP string) {
	// Skip network validation in CI environment as pod IPs are not directly accessible
	// from the test runner. In a real cluster, this would validate connectivity.
	if os.Getenv("CI") == "true" {
		t.Logf("Skipping direct pod connectivity test in CI environment for pod %s", podIP)
		return
	}
	
	// For mock deployment, verify the main API port instead of KV events port
	// The mock app doesn't implement real KV events functionality
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:8000", podIP), 5*time.Second)
	if err == nil {
		conn.Close()
		t.Logf("Successfully connected to API endpoint on pod %s", podIP)
	} else {
		t.Errorf("Failed to connect to API endpoint on pod %s: %v", podIP, err)
	}
}


// ScaleDeployment scales a deployment to the specified replicas
func (h *KVEventTestHelper) ScaleDeployment(t *testing.T, name string, replicas int32) {
	deployment, err := h.k8sClient.AppsV1().Deployments(h.namespace).Get(context.TODO(), name, metav1.GetOptions{})
	assert.NoError(t, err, "Failed to get deployment %s", name)
	
	deployment.Spec.Replicas = &replicas
	_, err = h.k8sClient.AppsV1().Deployments(h.namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
	assert.NoError(t, err, "Failed to scale deployment %s to %d replicas", name, replicas)
	
	t.Logf("Scaled deployment %s to %d replicas", name, replicas)
}

// DeletePod deletes a specific pod
func (h *KVEventTestHelper) DeletePod(t *testing.T, podName string) {
	err := h.k8sClient.CoreV1().Pods(h.namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
	assert.NoError(t, err, "Failed to delete pod %s", podName)
	
	t.Logf("Deleted pod %s", podName)
}


// CleanupDeployments cleans up all test deployments
func (h *KVEventTestHelper) CleanupDeployments(t *testing.T) {
	for _, deployment := range h.deployments {
		err := h.k8sClient.AppsV1().Deployments(h.namespace).Delete(context.TODO(), deployment.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Logf("Failed to delete deployment %s: %v", deployment.Name, err)
		}
	}
}

// WaitForPodDeletion waits for a pod to be deleted
func (h *KVEventTestHelper) WaitForPodDeletion(t *testing.T, podName string, timeout time.Duration) {
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			_, err := h.k8sClient.CoreV1().Pods(h.namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				// Pod is deleted
				return true, nil
			}
			return false, nil
		})
	
	assert.NoError(t, err, "Timeout waiting for pod %s to be deleted", podName)
}

// CreateService creates a service for the vLLM deployment
func (h *KVEventTestHelper) CreateService(t *testing.T, name string) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: h.namespace,
			Labels: map[string]string{
				"model.aibrix.ai/name": h.modelName,
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{Name: "serve", Port: 8000, Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(8000)},
				{Name: "kv-events", Port: kvEventsPort, Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(kvEventsPort)},
				{Name: "kv-replay", Port: kvReplayPort, Protocol: v1.ProtocolTCP, TargetPort: intstr.FromInt(kvReplayPort)},
			},
			Selector: map[string]string{
				"model.aibrix.ai/name": h.modelName,
			},
			Type: v1.ServiceTypeClusterIP,
		},
	}
	
	created, err := h.k8sClient.CoreV1().Services(h.namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	assert.NoError(t, err, "Failed to create service %s", name)
	
	return created
}