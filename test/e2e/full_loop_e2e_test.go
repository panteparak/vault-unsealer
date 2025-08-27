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

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/api/resource"
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
	Sealed      bool   `json:"sealed"`
	T           int    `json:"t"`
	N           int    `json:"n"`
	Progress    int    `json:"progress"`
	Initialized bool   `json:"initialized"`
	Version     string `json:"version"`
}

func TestFullLoopE2E(t *testing.T) {
	t.Log("🔄 === FULL RECONCILIATION LOOP E2E TEST STARTING ===")
	t.Log("This test validates the complete operator workflow:")
	t.Log("  • K3s cluster deployment")
	t.Log("  • Vault deployment and initialization") 
	t.Log("  • Operator deployment with controller manager")
	t.Log("  • VaultUnsealer CRD creation")
	t.Log("  • Controller reconciliation and unsealing")
	t.Log("  • Status updates and event handling")
	t.Log("  • Failure recovery and re-reconciliation")
	
	startTime := time.Now()
	t.Logf("🕐 Full loop test started at: %v", startTime.Format(time.RFC3339))

	ctx := context.Background()
	defer func() {
		totalDuration := time.Since(startTime)
		t.Logf("📊 Total full loop E2E test time: %v", totalDuration)
		t.Log("🎉 === FULL RECONCILIATION LOOP E2E TEST COMPLETED ===")
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
	t.Log("📦 STEP 2: Starting K3s container with custom configuration...")
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
			Cmd:          []string{"server", "--disable=traefik", "--disable=servicelb", "--disable=metrics-server", "--disable=local-storage", "--disable=coredns"},
			Networks:     []string{dockerNetwork.Name},
			NetworkAliases: map[string][]string{
				dockerNetwork.Name: {"k3s"},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("Node controller sync successful"),
				wait.ForListeningPort("6443/tcp"),
			).WithDeadline(3 * time.Minute),
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

	// Step 3: Setup Kubernetes clients
	t.Log("🔗 STEP 3: Setting up Kubernetes clients...")
	stepStart = time.Now()

	kubeConfigReader, err := k3sContainer.CopyFileFromContainer(ctx, "/output/kubeconfig.yaml")
	if err != nil {
		t.Fatalf("❌ Failed to get kubeconfig: %v", err)
	}
	defer kubeConfigReader.Close()

	kubeConfigData, err := io.ReadAll(kubeConfigReader)
	if err != nil {
		t.Fatalf("❌ Failed to read kubeconfig: %v", err)
	}

	cleanedKubeConfig, err := cleanKubeconfig(kubeConfigData)
	if err != nil {
		t.Fatalf("❌ Failed to clean kubeconfig: %v", err)
	}

	k3sPort, err := k3sContainer.MappedPort(ctx, "6443")
	if err != nil {
		t.Fatalf("❌ Failed to get K3s port: %v", err)
	}

	kubeConfigStr := string(cleanedKubeConfig)
	kubeConfigStr = strings.ReplaceAll(kubeConfigStr, "https://127.0.0.1:6443", fmt.Sprintf("https://127.0.0.1:%s", k3sPort.Port()))

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

	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		t.Fatalf("❌ Failed to build kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("❌ Failed to create Kubernetes client: %v", err)
	}

	// Setup controller-runtime client with all required schemes
	scheme := runtime.NewScheme()
	opsv1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	appsv1.AddToScheme(scheme)
	rbacv1.AddToScheme(scheme)
	apiextensionsv1.AddToScheme(scheme)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("❌ Failed to create controller-runtime client: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 3 COMPLETED: Kubernetes clients configured (took %v)", stepDuration)

	// Step 4: Wait for API server readiness
	t.Log("⏳ STEP 4: Waiting for API server to be fully ready...")
	stepStart = time.Now()

	if err := waitForAPIServer(ctx, clientset, 3*time.Minute); err != nil {
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
	t.Logf("✅ STEP 5 COMPLETED: CRDs installed and ready (took %v)", stepDuration)

	// Step 6: Deploy Vault in production mode
	t.Log("🏛️ STEP 6: Deploying production HashiCorp Vault...")
	stepStart = time.Now()

	vaultContainer, vaultURL, vaultKeys, rootToken, err := deployProductionVault(ctx, dockerNetwork)
	if err != nil {
		t.Fatalf("❌ Failed to deploy Vault: %v", err)
	}
	defer func() {
		t.Log("🧹 CLEANUP: Terminating Vault container...")
		if err := vaultContainer.Terminate(ctx); err != nil {
			t.Logf("⚠️ Failed to terminate Vault container: %v", err)
		}
	}()

	t.Logf("🔑 Vault initialized with %d keys and sealed", len(vaultKeys))

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 6 COMPLETED: Production Vault deployed and sealed (took %v)", stepDuration)

	// Step 7: Create namespace and secrets
	t.Log("🏗️ STEP 7: Creating operator namespace and secrets...")
	stepStart = time.Now()

	if err := createNamespaceAndSecrets(ctx, k8sClient, vaultKeys); err != nil {
		t.Fatalf("❌ Failed to create namespace and secrets: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 7 COMPLETED: Namespace and secrets created (took %v)", stepDuration)

	// Step 8: Deploy Vault service in Kubernetes
	t.Log("🚀 STEP 8: Creating Vault service and endpoints in Kubernetes...")
	stepStart = time.Now()

	if err := createVaultServiceAndEndpoints(ctx, k8sClient, dockerNetwork.Name); err != nil {
		t.Fatalf("❌ Failed to create Vault service: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 8 COMPLETED: Vault service created (took %v)", stepDuration)

	// Step 9: Build and load operator image
	t.Log("🐳 STEP 9: Building and loading operator image into K3s...")
	stepStart = time.Now()

	if err := buildAndLoadOperatorImage(ctx, k3sContainer); err != nil {
		t.Fatalf("❌ Failed to build/load operator image: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 9 COMPLETED: Operator image ready in K3s (took %v)", stepDuration)

	// Step 10: Deploy the operator with full RBAC
	t.Log("🤖 STEP 10: Deploying Vault Unsealer operator...")
	stepStart = time.Now()

	if err := deployFullOperator(ctx, k8sClient); err != nil {
		t.Fatalf("❌ Failed to deploy operator: %v", err)
	}

	// Wait for operator to be ready and running
	if err := waitForOperatorReady(ctx, clientset, 5*time.Minute); err != nil {
		t.Fatalf("❌ Operator not ready: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 10 COMPLETED: Operator deployed and running (took %v)", stepDuration)

	// Step 11: Verify Vault is sealed before starting test
	t.Log("🔒 STEP 11: Verifying Vault is sealed before test...")
	stepStart = time.Now()

	if sealed, err := checkVaultSealStatus(vaultURL); err != nil {
		t.Fatalf("❌ Failed to check Vault seal status: %v", err)
	} else if !sealed {
		t.Fatal("❌ Vault should be sealed before starting the operator test")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 11 COMPLETED: Vault confirmed sealed (took %v)", stepDuration)

	// Step 12: Create VaultUnsealer resource and watch reconciliation
	t.Log("📜 STEP 12: Creating VaultUnsealer resource to trigger reconciliation...")
	stepStart = time.Now()

	vaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "production-vault-unsealer",
			Namespace: "vault-system",
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				URL: "http://vault.vault-system.svc.cluster.local:8200",
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
		t.Fatalf("❌ Failed to create VaultUnsealer resource: %v", err)
	}

	t.Log("✅ VaultUnsealer resource created - controller should start reconciling!")

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 12 COMPLETED: VaultUnsealer resource created (took %v)", stepDuration)

	// Step 13: Monitor controller reconciliation and wait for unsealing
	t.Log("🔍 STEP 13: Monitoring controller reconciliation and unsealing...")
	stepStart = time.Now()

	// Monitor controller logs for reconciliation activity
	t.Log("📋 Monitoring controller logs for reconciliation activity...")
	go func() {
		for i := 0; i < 30; i++ { // Monitor for 5 minutes max
			time.Sleep(10 * time.Second)
			
			// Get controller logs
			pods, err := clientset.CoreV1().Pods("vault-system").List(ctx, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=vault-unsealer",
			})
			if err == nil && len(pods.Items) > 0 {
				podName := pods.Items[0].Name
				logReq := clientset.CoreV1().Pods("vault-system").GetLogs(podName, &corev1.PodLogOptions{
					TailLines: int64Ptr(10),
				})
				
				logs, err := logReq.Stream(ctx)
				if err == nil {
					logData, _ := io.ReadAll(logs)
					if len(logData) > 0 {
						t.Logf("🤖 Controller logs (last 10 lines):\n%s", string(logData))
					}
					logs.Close()
				}
			}
		}
	}()

	// Wait for automatic unsealing by controller
	t.Log("⏳ Waiting for controller to automatically unseal Vault...")
	
	unsealed := false
	for i := 0; i < 60; i++ { // Wait up to 10 minutes
		time.Sleep(10 * time.Second)
		
		sealed, err := checkVaultSealStatus(vaultURL)
		if err != nil {
			t.Logf("⚠️ Error checking seal status (attempt %d/60): %v", i+1, err)
			continue
		}
		
		if !sealed {
			unsealed = true
			t.Logf("🎉 SUCCESS! Vault automatically unsealed by controller after %v", time.Since(stepStart))
			break
		}
		
		t.Logf("⏳ Vault still sealed, waiting... (attempt %d/60)", i+1)
	}

	if !unsealed {
		t.Fatal("❌ Controller failed to unseal Vault within timeout period")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 13 COMPLETED: Controller successfully unsealed Vault (took %v)", stepDuration)

	// Step 14: Verify VaultUnsealer status was updated by controller
	t.Log("📊 STEP 14: Verifying VaultUnsealer status updates...")
	stepStart = time.Now()

	// Get updated VaultUnsealer resource
	updatedUnsealer := &opsv1alpha1.VaultUnsealer{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      "production-vault-unsealer",
		Namespace: "vault-system",
	}, updatedUnsealer); err != nil {
		t.Fatalf("❌ Failed to get updated VaultUnsealer: %v", err)
	}

	// Verify status fields were populated by controller
	t.Logf("📋 VaultUnsealer Status:")
	t.Logf("  • Pods Checked: %v", updatedUnsealer.Status.PodsChecked)
	t.Logf("  • Unsealed Pods: %v", updatedUnsealer.Status.UnsealedPods)
	t.Logf("  • Last Reconcile: %v", updatedUnsealer.Status.LastReconcileTime)
	t.Logf("  • Conditions: %d", len(updatedUnsealer.Status.Conditions))

	// Basic validation that controller updated the status
	if len(updatedUnsealer.Status.Conditions) == 0 {
		t.Error("❌ Expected controller to populate status conditions")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 14 COMPLETED: Status verification (took %v)", stepDuration)

	// Step 15: Test failure recovery - seal vault and verify controller re-unseals
	t.Log("💥 STEP 15: Testing failure recovery and re-reconciliation...")
	stepStart = time.Now()

	// Seal the vault again to test recovery
	t.Log("🔒 Re-sealing Vault to test controller recovery...")
	if err := sealVaultWithToken(vaultURL, rootToken); err != nil {
		t.Fatalf("❌ Failed to re-seal Vault: %v", err)
	}

	// Wait for controller to detect and re-unseal
	t.Log("⏳ Waiting for controller to detect seal and re-unseal...")
	
	reUnsealed := false
	for i := 0; i < 30; i++ { // Wait up to 5 minutes for recovery
		time.Sleep(10 * time.Second)
		
		sealed, err := checkVaultSealStatus(vaultURL)
		if err != nil {
			t.Logf("⚠️ Error checking recovery seal status (attempt %d/30): %v", i+1, err)
			continue
		}
		
		if !sealed {
			reUnsealed = true
			t.Logf("🎉 SUCCESS! Controller re-unsealed Vault after failure in %v", time.Since(stepStart))
			break
		}
		
		t.Logf("⏳ Vault still sealed during recovery, waiting... (attempt %d/30)", i+1)
	}

	if !reUnsealed {
		t.Error("❌ Controller failed to re-unseal Vault during failure recovery")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 15 COMPLETED: Failure recovery validated (took %v)", stepDuration)

	// Step 16: Cleanup testing - verify finalizers work
	t.Log("🧹 STEP 16: Testing resource cleanup and finalizers...")
	stepStart = time.Now()

	if err := testResourceCleanup(ctx, k8sClient); err != nil {
		t.Fatalf("❌ Cleanup testing failed: %v", err)
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 16 COMPLETED: Cleanup testing passed (took %v)", stepDuration)

	// Final validation
	t.Log("")
	t.Log("🎉 === FULL RECONCILIATION LOOP VALIDATION COMPLETE ===")
	t.Log("✅ Kubernetes cluster deployed successfully")
	t.Log("✅ Production Vault deployed and initialized") 
	t.Log("✅ Operator deployed with complete RBAC")
	t.Log("✅ VaultUnsealer resource created successfully")
	t.Log("✅ Controller reconciliation loop working correctly")
	t.Log("✅ Automatic unsealing performed by controller")
	t.Log("✅ Status updates handled properly")
	t.Log("✅ Failure recovery and re-reconciliation verified")
	t.Log("✅ Resource cleanup and finalizers working")
	t.Log("")
	t.Log("🏆 The Vault Unsealer operator full workflow is FULLY VALIDATED!")
}

// Helper functions for the full loop test

func deployProductionVault(ctx context.Context, dockerNetwork *testcontainers.DockerNetwork) (testcontainers.Container, string, []string, string, error) {
	vaultContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
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
				wait.ForLog("Vault server started!"),
				wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp").WithStatusCodeMatcher(func(status int) bool {
					return status == 501 || status == 200
				}),
			).WithDeadline(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("failed to start Vault container: %w", err)
	}

	vaultPort, err := vaultContainer.MappedPort(ctx, "8200")
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("failed to get Vault port: %w", err)
	}
	vaultURL := fmt.Sprintf("http://127.0.0.1:%s", vaultPort.Port())

	// Initialize Vault
	vaultKeys, rootToken, err := initializeVault(vaultURL)
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("failed to initialize Vault: %w", err)
	}

	// Seal Vault for testing
	if err := sealVaultWithToken(vaultURL, rootToken); err != nil {
		return nil, "", nil, "", fmt.Errorf("failed to seal Vault: %w", err)
	}

	return vaultContainer, vaultURL, vaultKeys, rootToken, nil
}

func createNamespaceAndSecrets(ctx context.Context, k8sClient client.Client, vaultKeys []string) error {
	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-system",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Create unsealing secrets
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

func createVaultServiceAndEndpoints(ctx context.Context, k8sClient client.Client, networkName string) error {
	// Create service
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

	// Create endpoints pointing to Docker container
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault",
			Namespace: "vault-system",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "vault", // Use Docker network alias
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

	// Create mock Vault pods for selector matching
	for i := 0; i < 2; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("vault-%d", i),
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
			return fmt.Errorf("failed to create vault pod %d: %w", i, err)
		}
	}

	return nil
}

func buildAndLoadOperatorImage(ctx context.Context, k3sContainer testcontainers.Container) error {
	// Build the operator binary first
	buildCmd := exec.Command("make", "build")
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build operator: %w", err)
	}

	// Build Docker image
	dockerCmd := exec.Command("make", "docker-build-e2e")
	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("failed to build Docker image: %w", err)
	}

	// Save and load image into K3s
	return loadImageIntoK3s(ctx, k3sContainer)
}

func deployFullOperator(ctx context.Context, k8sClient client.Client) error {
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

	// Create cluster role with all required permissions
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-unsealer-manager-role",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "secrets", "events", "services", "endpoints"},
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
							Name:            "manager",
							Image:           "controller:latest",
							ImagePullPolicy: corev1.PullNever, // Use local image
							Command: []string{
								"/manager",
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect=false", // Disable leader election for single replica
								"--zap-log-level=debug", // Enable debug logging
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
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
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
	return utilwait.PollImmediate(10*time.Second, timeout, func() (bool, error) {
		deployment, err := clientset.AppsV1().Deployments("vault-system").Get(ctx, "vault-unsealer-controller-manager", metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil // Keep waiting
			}
			return false, err
		}

		// Check if deployment is ready
		if deployment.Status.ReadyReplicas > 0 && 
		   deployment.Status.Replicas == deployment.Status.ReadyReplicas &&
		   deployment.Status.UnavailableReplicas == 0 {
			return true, nil
		}

		return false, nil
	})
}

func checkVaultSealStatus(vaultURL string) (bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	
	resp, err := client.Get(vaultURL + "/v1/sys/seal-status")
	if err != nil {
		return false, fmt.Errorf("failed to get seal status: %w", err)
	}
	defer resp.Body.Close()

	var status VaultSealStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode seal status: %w", err)
	}

	return status.Sealed, nil
}

func sealVaultWithToken(vaultURL, rootToken string) error {
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

func testResourceCleanup(ctx context.Context, k8sClient client.Client) error {
	// Test finalizer handling by deleting VaultUnsealer
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "production-vault-unsealer", Namespace: "vault-system"}, vaultUnsealer); err != nil {
		return fmt.Errorf("failed to get VaultUnsealer: %w", err)
	}

	if err := k8sClient.Delete(ctx, vaultUnsealer); err != nil {
		return fmt.Errorf("failed to delete VaultUnsealer: %w", err)
	}

	// Wait for finalizer to complete and resource to be deleted
	return utilwait.PollImmediate(5*time.Second, 60*time.Second, func() (bool, error) {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: "production-vault-unsealer", Namespace: "vault-system"}, vaultUnsealer)
		if errors.IsNotFound(err) {
			return true, nil // Successfully deleted
		}
		if err != nil {
			return false, err
		}
		return false, nil // Still exists, keep waiting
	})
}

func int32Ptr(i int32) *int32 {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

// Helper functions copied from other test files

func cleanKubeconfig(data []byte) ([]byte, error) {
	re := regexp.MustCompile(`(?s)apiVersion:.*`)
	matches := re.Find(data)
	if matches == nil {
		return nil, fmt.Errorf("could not find valid kubeconfig content")
	}
	return matches, nil
}

func waitForAPIServer(ctx context.Context, clientset kubernetes.Interface, timeout time.Duration) error {
	return utilwait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		_, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil // Keep waiting
		}
		return true, nil
	})
}

func installCRDs(ctx context.Context, k8sClient client.Client) error {
	// Read CRD from file
	crdPath := "../../config/crd/bases/ops.autounseal.vault.io_vaultunsealers.yaml"
	crdData, err := os.ReadFile(crdPath)
	if err != nil {
		return fmt.Errorf("failed to read CRD file: %w", err)
	}

	// Parse CRD
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(crdData, &crd); err != nil {
		return fmt.Errorf("failed to unmarshal CRD: %w", err)
	}

	// Create CRD
	if err := k8sClient.Create(ctx, &crd); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create CRD: %w", err)
	}

	// Wait for CRD to be established
	return utilwait.PollImmediate(2*time.Second, 60*time.Second, func() (bool, error) {
		var installedCRD apiextensionsv1.CustomResourceDefinition
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: crd.Name}, &installedCRD); err != nil {
			return false, nil
		}

		for _, condition := range installedCRD.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

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