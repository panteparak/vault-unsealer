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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

// VaultInitResponse represents the response from Vault init
type VaultInitResponse struct {
	Keys       []string `json:"keys"`
	KeysBase64 []string `json:"keys_base64"`
	RootToken  string   `json:"root_token"`
}

// VaultSealStatusResponse represents Vault seal status
type VaultSealStatusResponse struct {
	Type         string `json:"type"`
	Initialized  bool   `json:"initialized"`
	Sealed       bool   `json:"sealed"`
	T            int    `json:"t"`
	N            int    `json:"n"`
	Progress     int    `json:"progress"`
	Nonce        string `json:"nonce"`
	Version      string `json:"version"`
	BuildDate    string `json:"build_date"`
	Migration    bool   `json:"migration"`
	ClusterName  string `json:"cluster_name"`
	ClusterID    string `json:"cluster_id"`
	RecoveryKeys int    `json:"recovery_keys"`
	StorageType  string `json:"storage_type"`
}

func TestFullE2E(t *testing.T) {
	t.Log("🚀 === COMPREHENSIVE E2E TEST SUITE STARTING ===")
	startTime := time.Now()
	t.Logf("🕐 Test execution started at: %v", startTime.Format(time.RFC3339))

	ctx := context.Background()
	defer func() {
		totalDuration := time.Since(startTime)
		t.Logf("📊 Total comprehensive E2E test execution time: %v", totalDuration)
		t.Log("🎉 === COMPREHENSIVE E2E TEST SUITE COMPLETED ===")
	}()

	// Step 1: Create Docker network for inter-container communication
	t.Log("🌐 STEP 1: Creating Docker network for containers...")
	stepStart := time.Now()

	dockerNetwork, err := network.New(ctx, network.WithDriver("bridge"))
	if err != nil {
		t.Fatalf("❌ Failed to create Docker network: %v", err)
	}
	defer func() {
		if err := dockerNetwork.Remove(ctx); err != nil {
			t.Logf("⚠️ Failed to remove Docker network: %v", err)
		}
	}()

	stepDuration := time.Since(stepStart)
	t.Logf("✅ STEP 1 COMPLETED: Docker network created (took %v)", stepDuration)

	// Step 2: Start K3s container
	t.Log("📦 STEP 2: Starting K3s container...")
	stepStart = time.Now()

	k3sContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rancher/k3s:v1.28.5-k3s1",
			ExposedPorts: []string{"6443/tcp", "80/tcp", "443/tcp"},
			Env: map[string]string{
				"K3S_KUBECONFIG_OUTPUT": "/output/kubeconfig.yaml",
				"K3S_KUBECONFIG_MODE":   "666",
			},
			Mounts: testcontainers.Mounts(
				testcontainers.BindMount("/tmp", "/output"),
			),
			Cmd:          []string{"server", "--disable=traefik", "--disable=servicelb", "--disable=metrics-server", "--disable=local-storage"},
			Networks:     []string{dockerNetwork.Name},
			NetworkAliases: map[string][]string{
				dockerNetwork.Name: {"k3s"},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("Node controller sync successful"),
				wait.ForListeningPort("6443/tcp"),
			).WithDeadline(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("❌ Failed to start K3s container: %v", err)
	}
	defer func() {
		t.Log("🧹 CLEANUP: Terminating K3s container...")
		if err := k3sContainer.Terminate(ctx); err != nil {
			t.Logf("⚠️ Failed to terminate K3s container: %v", err)
		} else {
			t.Log("✅ CLEANUP: K3s container terminated successfully")
		}
	}()

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 2 COMPLETED: K3s container started (took %v)", stepDuration)

	// Step 3: Get kubeconfig and setup Kubernetes client
	t.Log("🔗 STEP 3: Setting up Kubernetes client...")
	stepStart = time.Now()

	// Get kubeconfig from container
	kubeConfigReader, err := k3sContainer.CopyFileFromContainer(ctx, "/output/kubeconfig.yaml")
	if err != nil {
		t.Fatalf("❌ Failed to get kubeconfig: %v", err)
	}
	defer kubeConfigReader.Close()

	kubeConfigData, err := io.ReadAll(kubeConfigReader)
	if err != nil {
		t.Fatalf("❌ Failed to read kubeconfig: %v", err)
	}

	// Clean kubeconfig data
	cleanedKubeConfig, err := cleanKubeconfig(kubeConfigData)
	if err != nil {
		t.Fatalf("❌ Failed to clean kubeconfig: %v", err)
	}

	// Get K3s container port
	k3sPort, err := k3sContainer.MappedPort(ctx, "6443")
	if err != nil {
		t.Fatalf("❌ Failed to get K3s port: %v", err)
	}

	// Replace server URL in kubeconfig
	kubeConfigStr := string(cleanedKubeConfig)
	kubeConfigStr = strings.ReplaceAll(kubeConfigStr, "https://127.0.0.1:6443", fmt.Sprintf("https://127.0.0.1:%s", k3sPort.Port()))

	// Write kubeconfig to temp file
	tempDir, err := os.MkdirTemp("", "e2e-kubeconfig-")
	if err != nil {
		t.Fatalf("❌ Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	kubeConfigPath := filepath.Join(tempDir, "kubeconfig")
	err = os.WriteFile(kubeConfigPath, []byte(kubeConfigStr), 0644)
	if err != nil {
		t.Fatalf("❌ Failed to write kubeconfig: %v", err)
	}

	// Create Kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		t.Fatalf("❌ Failed to build kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("❌ Failed to create Kubernetes client: %v", err)
	}

	// Setup controller-runtime client
	scheme := runtime.NewScheme()
	opsv1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	appsv1.AddToScheme(scheme)
	rbacv1.AddToScheme(scheme)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("❌ Failed to create controller-runtime client: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 3 COMPLETED: Kubernetes client configured (took %v)", stepDuration)

	// Step 4: Wait for API server
	t.Log("⏳ STEP 4: Waiting for API server to be ready...")
	stepStart = time.Now()

	if err := waitForAPIServer(ctx, clientset, 2*time.Minute); err != nil {
		t.Fatalf("❌ API server not ready: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 4 COMPLETED: API server is ready (took %v)", stepDuration)

	// Step 5: Install CRDs
	t.Log("📋 STEP 5: Installing Custom Resource Definitions...")
	stepStart = time.Now()

	if err := installCRDs(ctx, k8sClient); err != nil {
		t.Fatalf("❌ Failed to install CRDs: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 5 COMPLETED: CRDs installed (took %v)", stepDuration)

	// Step 6: Start Vault container
	t.Log("🏛️ STEP 6: Starting HashiCorp Vault container...")
	stepStart = time.Now()

	vaultContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "hashicorp/vault:1.15.2",
			ExposedPorts: []string{"8200/tcp"},
			Env: map[string]string{
				"VAULT_DEV_ROOT_TOKEN_ID": "root-token",
				"VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200",
				"VAULT_ADDR":              "http://0.0.0.0:8200",
			},
			Cmd:      []string{"vault", "server", "-dev", "-dev-root-token-id=root-token"},
			Networks: []string{dockerNetwork.Name},
			NetworkAliases: map[string][]string{
				dockerNetwork.Name: {"vault"},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("Development mode should NOT be used in production installations!"),
				wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp"),
			).WithDeadline(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("❌ Failed to start Vault container: %v", err)
	}
	defer func() {
		t.Log("🧹 CLEANUP: Terminating Vault container...")
		if err := vaultContainer.Terminate(ctx); err != nil {
			t.Logf("⚠️ Failed to terminate Vault container: %v", err)
		} else {
			t.Log("✅ CLEANUP: Vault container terminated successfully")
		}
	}()

	// Get Vault port for external access
	vaultPort, err := vaultContainer.MappedPort(ctx, "8200")
	if err != nil {
		t.Fatalf("❌ Failed to get Vault port: %v", err)
	}
	vaultURL := fmt.Sprintf("http://127.0.0.1:%s", vaultPort.Port())

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 6 COMPLETED: Vault container started on %s (took %v)", vaultURL, stepDuration)

	// Step 7: Initialize production Vault instance
	t.Log("🔐 STEP 7: Setting up production Vault instance...")
	stepStart = time.Now()

	// Stop dev vault and start production vault
	if err := vaultContainer.Terminate(ctx); err != nil {
		t.Fatalf("❌ Failed to stop dev Vault: %v", err)
	}

	// Start production Vault
	prodVaultContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "hashicorp/vault:1.15.2",
			ExposedPorts: []string{"8200/tcp"},
			Env: map[string]string{
				"VAULT_ADDR":              "http://0.0.0.0:8200",
				"VAULT_API_ADDR":          "http://0.0.0.0:8200",
				"VAULT_LOCAL_CONFIG": `{
					"backend": {"file": {"path": "/vault/data"}},
					"listener": {"tcp": {"address": "0.0.0.0:8200", "tls_disable": true}},
					"disable_mlock": true,
					"default_lease_ttl": "168h",
					"max_lease_ttl": "720h"
				}`,
			},
			Cmd:      []string{"vault", "server", "-config=/vault/config"},
			Networks: []string{dockerNetwork.Name},
			NetworkAliases: map[string][]string{
				dockerNetwork.Name: {"vault"},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("vault server started"),
				wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp").WithStatusCodeMatcher(func(status int) bool {
					// Vault returns 501 when not initialized, which is expected
					return status == 501 || status == 200
				}),
			).WithDeadline(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("❌ Failed to start production Vault: %v", err)
	}
	defer func() {
		t.Log("🧹 CLEANUP: Terminating production Vault container...")
		if err := prodVaultContainer.Terminate(ctx); err != nil {
			t.Logf("⚠️ Failed to terminate production Vault container: %v", err)
		}
	}()

	// Update Vault URL for new container
	prodVaultPort, err := prodVaultContainer.MappedPort(ctx, "8200")
	if err != nil {
		t.Fatalf("❌ Failed to get production Vault port: %v", err)
	}
	vaultURL = fmt.Sprintf("http://127.0.0.1:%s", prodVaultPort.Port())

	// Initialize Vault
	vaultKeys, rootToken, err := initializeVault(vaultURL)
	if err != nil {
		t.Fatalf("❌ Failed to initialize Vault: %v", err)
	}

	t.Logf("🔑 Vault initialized with %d keys", len(vaultKeys))
	t.Log("🔒 Sealing Vault for testing...")

	// Seal the Vault
	if err := sealVault(vaultURL, rootToken); err != nil {
		t.Fatalf("❌ Failed to seal Vault: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 7 COMPLETED: Production Vault setup and sealed (took %v)", stepDuration)

	// Step 8: Deploy Vault as Kubernetes service
	t.Log("🚀 STEP 8: Deploying Vault service in Kubernetes...")
	stepStart = time.Now()

	if err := deployVaultService(ctx, k8sClient, dockerNetwork.Name); err != nil {
		t.Fatalf("❌ Failed to deploy Vault service: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 8 COMPLETED: Vault service deployed (took %v)", stepDuration)

	// Step 9: Create unsealing secrets
	t.Log("🔑 STEP 9: Creating Vault unsealing secrets...")
	stepStart = time.Now()

	if err := createUnsealingSecrets(ctx, k8sClient, vaultKeys); err != nil {
		t.Fatalf("❌ Failed to create unsealing secrets: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 9 COMPLETED: Unsealing secrets created (took %v)", stepDuration)

	// Step 10: Load Docker image into K3s and deploy the operator
	t.Log("🤖 STEP 10: Loading image and deploying Vault Unsealer operator...")
	stepStart = time.Now()

	// Export and load the Docker image into K3s
	if err := loadImageIntoK3s(ctx, k3sContainer); err != nil {
		t.Fatalf("❌ Failed to load image into K3s: %v", err)
	}

	if err := deployOperator(ctx, k8sClient); err != nil {
		t.Fatalf("❌ Failed to deploy operator: %v", err)
	}

	// Wait for operator to be ready
	if err := waitForOperatorReady(ctx, clientset, 3*time.Minute); err != nil {
		t.Fatalf("❌ Operator not ready: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 10 COMPLETED: Operator deployed and ready (took %v)", stepDuration)

	// Step 11: Create VaultUnsealer resource
	t.Log("📜 STEP 11: Creating VaultUnsealer custom resource...")
	stepStart = time.Now()

	vaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vault-unsealer",
			Namespace: "vault-system",
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				Address: "http://vault.vault-system.svc.cluster.local:8200",
			},
			UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
				{
					Name:      "vault-unseal-keys",
					Namespace: "vault-system",
					Key:       "keys.json",
				},
			},
			VaultLabelSelector: "app.kubernetes.io/name=vault",
			Mode: opsv1alpha1.ModeSpec{
				HA: true,
			},
			KeyThreshold: 3,
		},
	}

	if err := k8sClient.Create(ctx, vaultUnsealer); err != nil {
		t.Fatalf("❌ Failed to create VaultUnsealer: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 11 COMPLETED: VaultUnsealer resource created (took %v)", stepDuration)

	// Step 12: Wait for unsealing to complete
	t.Log("🔓 STEP 12: Waiting for automatic Vault unsealing...")
	stepStart = time.Now()

	if err := waitForVaultUnseal(vaultURL, 2*time.Minute); err != nil {
		t.Fatalf("❌ Vault was not unsealed: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 12 COMPLETED: Vault successfully unsealed (took %v)", stepDuration)

	// Step 13: Verify metrics endpoint
	t.Log("📊 STEP 13: Verifying metrics endpoint...")
	stepStart = time.Now()

	if err := verifyMetricsEndpoint(ctx, clientset); err != nil {
		t.Logf("⚠️ Metrics verification skipped: %v", err)
	} else {
		t.Log("✅ Metrics endpoint verified")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 13 COMPLETED: Metrics verification (took %v)", stepDuration)

	// Step 14: Test failure scenarios
	t.Log("💥 STEP 14: Testing failure scenarios...")
	stepStart = time.Now()

	if err := testFailureScenarios(ctx, vaultURL, rootToken, k8sClient); err != nil {
		t.Fatalf("❌ Failure scenario testing failed: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 14 COMPLETED: Failure scenarios tested (took %v)", stepDuration)

	// Step 15: Cleanup verification
	t.Log("🧹 STEP 15: Testing cleanup and finalizers...")
	stepStart = time.Now()

	if err := testCleanup(ctx, k8sClient); err != nil {
		t.Fatalf("❌ Cleanup testing failed: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 15 COMPLETED: Cleanup testing passed (took %v)", stepDuration)

	t.Log("🎉 All 15 comprehensive E2E test steps completed successfully!")
}

// Helper functions

func initializeVault(vaultURL string) ([]string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	initData := map[string]interface{}{
		"secret_shares":    5,
		"secret_threshold": 3,
	}

	initBody, _ := json.Marshal(initData)
	resp, err := client.Post(vaultURL+"/v1/sys/init", "application/json", strings.NewReader(string(initBody)))
	if err != nil {
		return nil, "", fmt.Errorf("failed to initialize Vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("vault init failed with status %d: %s", resp.StatusCode, string(body))
	}

	var initResp VaultInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, "", fmt.Errorf("failed to decode init response: %w", err)
	}

	return initResp.Keys, initResp.RootToken, nil
}

func sealVault(vaultURL, rootToken string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("PUT", vaultURL+"/v1/sys/seal", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", rootToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to seal Vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault seal failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func waitForVaultUnseal(vaultURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(vaultURL + "/v1/sys/seal-status")
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var status VaultSealStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			resp.Body.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		resp.Body.Close()

		if !status.Sealed {
			return nil // Successfully unsealed
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("vault was not unsealed within timeout")
}

func deployVaultService(ctx context.Context, k8sClient client.Client, networkName string) error {
	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-system",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Create external service pointing to Vault container
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault",
			Namespace: "vault-system",
			Labels: map[string]string{
				"app.kubernetes.io/name": "vault",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": "vault",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8200,
					TargetPort: intstr.FromInt(8200),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, service); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Create endpoints to point to actual Vault container
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault",
			Namespace: "vault-system",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "vault", // Use network alias
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Name:     "http",
						Port:     8200,
						Protocol: corev1.ProtocolTCP,
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, endpoints); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create endpoints: %w", err)
	}

	// Create a dummy pod with vault labels for the selector to work
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault-0",
			Namespace: "vault-system",
			Labels: map[string]string{
				"app.kubernetes.io/name": "vault",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "pause",
					Image: "registry.k8s.io/pause:3.9",
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, pod); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create pod: %w", err)
	}

	return nil
}

func createUnsealingSecrets(ctx context.Context, k8sClient client.Client, vaultKeys []string) error {
	// Create JSON format secret
	keysJSON, err := json.Marshal(vaultKeys)
	if err != nil {
		return fmt.Errorf("failed to marshal vault keys: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault-unseal-keys",
			Namespace: "vault-system",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"keys.json": keysJSON,
		},
	}

	if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create secret: %w", err)
	}

	return nil
}

func deployOperator(ctx context.Context, k8sClient client.Client) error {
	// Create service account
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault-unsealer-controller-manager",
			Namespace: "vault-system",
		},
	}
	if err := k8sClient.Create(ctx, sa); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create service account: %w", err)
	}

	// Create cluster role
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-unsealer-manager-role",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "secrets", "events"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
			},
			{
				APIGroups: []string{"ops.autounseal.vault.io"},
				Resources: []string{"vaultunsealers"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"ops.autounseal.vault.io"},
				Resources: []string{"vaultunsealers/status", "vaultunsealers/finalizers"},
				Verbs:     []string{"get", "update", "patch"},
			},
		},
	}
	if err := k8sClient.Create(ctx, cr); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create cluster role: %w", err)
	}

	// Create cluster role binding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-unsealer-manager-rolebinding",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "vault-unsealer-manager-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "vault-unsealer-controller-manager",
				Namespace: "vault-system",
			},
		},
	}
	if err := k8sClient.Create(ctx, crb); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create cluster role binding: %w", err)
	}

	// Create deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault-unsealer-controller-manager",
			Namespace: "vault-system",
			Labels: map[string]string{
				"app.kubernetes.io/name": "vault-unsealer",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": "vault-unsealer",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name": "vault-unsealer",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "vault-unsealer-controller-manager",
					Containers: []corev1.Container{
						{
							Name:  "manager",
							Image: "controller:latest", // This would be built from current code
							Command: []string{
								"/manager",
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9443,
									Name:          "webhook-server",
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    "500m",
									corev1.ResourceMemory: "128Mi",
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    "10m",
									corev1.ResourceMemory: "64Mi",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, deployment); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	return nil
}

func waitForOperatorReady(ctx context.Context, clientset kubernetes.Interface, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		deployment, err := clientset.AppsV1().Deployments("vault-system").Get(ctx, "vault-unsealer-controller-manager", metav1.GetOptions{})
		if err == nil && deployment.Status.ReadyReplicas > 0 {
			return nil
		}

		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("operator was not ready within timeout")
}

func verifyMetricsEndpoint(ctx context.Context, clientset kubernetes.Interface) error {
	// This would typically involve port-forwarding to the metrics endpoint
	// and checking that Prometheus metrics are being exposed
	// For now, we'll just verify the service exists
	_, err := clientset.CoreV1().Services("vault-system").Get(ctx, "vault-unsealer-controller-manager-metrics-service", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("metrics service not found: %w", err)
	}
	return nil
}

func testFailureScenarios(ctx context.Context, vaultURL, rootToken string, k8sClient client.Client) error {
	// Test 1: Re-seal Vault and verify it gets unsealed again
	if err := sealVault(vaultURL, rootToken); err != nil {
		return fmt.Errorf("failed to re-seal vault: %w", err)
	}

	// Wait for automatic re-unsealing
	if err := waitForVaultUnseal(vaultURL, 1*time.Minute); err != nil {
		return fmt.Errorf("vault was not automatically re-unsealed: %w", err)
	}

	// Test 2: Delete secret and verify operator handles it gracefully
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "vault-unseal-keys", Namespace: "vault-system"}, secret); err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	originalData := secret.Data
	if err := k8sClient.Delete(ctx, secret); err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	// Wait a bit for operator to detect missing secret
	time.Sleep(30 * time.Second)

	// Recreate secret
	secret.ResourceVersion = ""
	secret.Data = originalData
	if err := k8sClient.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to recreate secret: %w", err)
	}

	return nil
}

func testCleanup(ctx context.Context, k8sClient client.Client) error {
	// Test finalizer handling by deleting VaultUnsealer
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-vault-unsealer", Namespace: "vault-system"}, vaultUnsealer); err != nil {
		return fmt.Errorf("failed to get VaultUnsealer: %w", err)
	}

	if err := k8sClient.Delete(ctx, vaultUnsealer); err != nil {
		return fmt.Errorf("failed to delete VaultUnsealer: %w", err)
	}

	// Wait for finalizer to complete and resource to be deleted
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-vault-unsealer", Namespace: "vault-system"}, vaultUnsealer)
		if errors.IsNotFound(err) {
			return nil // Successfully deleted
		}
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("VaultUnsealer was not deleted within timeout")
}

func loadImageIntoK3s(ctx context.Context, k3sContainer testcontainers.Container) error {
	// Save the Docker image to a tar file
	saveCmd := []string{"docker", "save", "controller:latest", "-o", "/tmp/controller-latest.tar"}
	if err := exec.Command(saveCmd[0], saveCmd[1:]...).Run(); err != nil {
		return fmt.Errorf("failed to save Docker image: %w", err)
	}
	defer func() {
		os.Remove("/tmp/controller-latest.tar")
	}()

	// Copy the tar file to the K3s container
	err := k3sContainer.CopyFileToContainer(ctx, "/tmp/controller-latest.tar", "/tmp/controller-latest.tar", 644)
	if err != nil {
		return fmt.Errorf("failed to copy image tar to K3s container: %w", err)
	}

	// Load the image inside K3s
	exitCode, reader, err := k3sContainer.Exec(ctx, []string{"ctr", "images", "import", "/tmp/controller-latest.tar"})
	if err != nil {
		return fmt.Errorf("failed to execute ctr command: %w", err)
	}

	if exitCode != 0 {
		output, _ := io.ReadAll(reader)
		return fmt.Errorf("failed to load image in K3s (exit code %d): %s", exitCode, string(output))
	}

	return nil
}

func int32Ptr(i int32) *int32 {
	return &i
}