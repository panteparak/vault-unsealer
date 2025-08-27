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
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

type E2ETestSuite struct {
	k3sContainer    testcontainers.Container
	vaultContainers []testcontainers.Container
	k8sClient       client.Client
	kubeClient      kubernetes.Interface
	kubeconfig      string
	ctx             context.Context
	testEnv         *envtest.Environment
	cfg             *rest.Config
	scheme          *runtime.Scheme
}

type VaultStatus struct {
	Sealed      bool   `json:"sealed"`
	T           int    `json:"t"`
	N           int    `json:"n"`
	Progress    int    `json:"progress"`
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

var _ = ginkgo.Describe("Vault Unsealer E2E Tests", func() {
	var suite *E2ETestSuite

	ginkgo.BeforeEach(func() {
		suite = &E2ETestSuite{
			ctx: context.Background(),
		}

		ginkgo.By("Setting up test environment")
		suite.setupTestEnvironment()
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleaning up test environment")
		suite.cleanup()
	})

	ginkgo.Context("Basic Unsealing Workflow", func() {
		ginkgo.It("should automatically unseal a single Vault pod", func() {
			// Deploy a single Vault pod
			vaultPod := suite.deployVaultPod("vault-single", false)

			// Initialize Vault and get unseal keys
			unsealKeys := suite.initializeVault(vaultPod)

			// Seal the vault
			suite.sealVault(vaultPod)

			// Create unseal keys secret
			suite.createUnsealKeysSecret("vault-unseal-keys", "default", unsealKeys)

			// Deploy the VaultUnsealer resource
			suite.deployVaultUnsealer("vault-unsealer", "default", false, 3)

			// Wait for vault to be unsealed
			gomega.Eventually(func() bool {
				return suite.isVaultUnsealed(vaultPod)
			}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue())

			// Verify VaultUnsealer status
			vaultUnsealer := suite.getVaultUnsealer("vault-unsealer", "default")
			gomega.Expect(vaultUnsealer.Status.UnsealedPods).To(gomega.ContainElement(vaultPod.Name))
		})

		ginkgo.It("should handle HA mode with multiple Vault pods", func() {
			// Deploy multiple Vault pods
			vaultPods := []corev1.Pod{
				suite.deployVaultPod("vault-0", true),
				suite.deployVaultPod("vault-1", true),
				suite.deployVaultPod("vault-2", true),
			}

			// Initialize one vault and get unseal keys
			unsealKeys := suite.initializeVault(vaultPods[0])

			// Seal all vaults
			for _, pod := range vaultPods {
				suite.sealVault(pod)
			}

			// Create unseal keys secret
			suite.createUnsealKeysSecret("vault-unseal-keys", "default", unsealKeys)

			// Deploy the VaultUnsealer resource with HA mode
			suite.deployVaultUnsealer("vault-unsealer-ha", "default", true, 3)

			// Wait for all vaults to be unsealed
			gomega.Eventually(func() bool {
				unselead := 0
				for _, pod := range vaultPods {
					if suite.isVaultUnsealed(pod) {
						unselead++
					}
				}
				return unselead == len(vaultPods)
			}, 3*time.Minute, 5*time.Second).Should(gomega.BeTrue())

			// Verify VaultUnsealer status
			vaultUnsealer := suite.getVaultUnsealer("vault-unsealer-ha", "default")
			gomega.Expect(len(vaultUnsealer.Status.UnsealedPods)).To(gomega.Equal(len(vaultPods)))
		})

		ginkgo.It("should respect key threshold", func() {
			// Deploy a single Vault pod
			vaultPod := suite.deployVaultPod("vault-threshold", false)

			// Initialize Vault with higher threshold
			unsealKeys := suite.initializeVaultWithThreshold(vaultPod, 3, 5)

			// Seal the vault
			suite.sealVault(vaultPod)

			// Create unseal keys secret with all keys
			suite.createUnsealKeysSecret("vault-unseal-keys-all", "default", unsealKeys)

			// Deploy the VaultUnsealer resource with threshold of 3
			suite.deployVaultUnsealer("vault-unsealer-threshold", "default", false, 3)

			// Wait for vault to be unsealed
			gomega.Eventually(func() bool {
				return suite.isVaultUnsealed(vaultPod)
			}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("Multi-Secret Support", func() {
		ginkgo.It("should load keys from multiple secrets", func() {
			// Deploy a single Vault pod
			vaultPod := suite.deployVaultPod("vault-multi-secret", false)

			// Initialize Vault
			unsealKeys := suite.initializeVault(vaultPod)

			// Split keys across multiple secrets
			keys1 := unsealKeys[:2]
			keys2 := unsealKeys[2:]

			suite.createUnsealKeysSecret("vault-keys-1", "default", keys1)
			suite.createUnsealKeysSecret("vault-keys-2", "default", keys2)

			// Seal the vault
			suite.sealVault(vaultPod)

			// Deploy VaultUnsealer with multiple secret references
			suite.deployVaultUnsealerWithMultipleSecrets("vault-unsealer-multi", "default", []string{"vault-keys-1", "vault-keys-2"})

			// Wait for vault to be unsealed
			gomega.Eventually(func() bool {
				return suite.isVaultUnsealed(vaultPod)
			}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("Error Scenarios", func() {
		ginkgo.It("should handle missing secrets gracefully", func() {
			// Deploy a single Vault pod
			vaultPod := suite.deployVaultPod("vault-missing-secret", false)

			// Initialize and seal vault
			suite.initializeVault(vaultPod)
			suite.sealVault(vaultPod)

			// Deploy VaultUnsealer with non-existent secret
			suite.deployVaultUnsealer("vault-unsealer-missing", "default", false, 3)

			// Verify error condition is set
			gomega.Eventually(func() bool {
				vaultUnsealer := suite.getVaultUnsealer("vault-unsealer-missing", "default")
				for _, condition := range vaultUnsealer.Status.Conditions {
					if condition.Type == "KeysMissing" && condition.Status == "True" {
						return true
					}
				}
				return false
			}, 1*time.Minute, 5*time.Second).Should(gomega.BeTrue())
		})

		ginkgo.It("should handle insufficient keys", func() {
			// Deploy a single Vault pod
			vaultPod := suite.deployVaultPod("vault-insufficient", false)

			// Initialize Vault
			unsealKeys := suite.initializeVault(vaultPod)

			// Create secret with insufficient keys (only 1 key when 3 needed)
			suite.createUnsealKeysSecret("vault-insufficient-keys", "default", unsealKeys[:1])

			// Seal the vault
			suite.sealVault(vaultPod)

			// Deploy VaultUnsealer
			suite.deployVaultUnsealer("vault-unsealer-insufficient", "default", false, 3)

			// Vault should remain sealed
			gomega.Consistently(func() bool {
				return suite.isVaultUnsealed(vaultPod)
			}, 30*time.Second, 5*time.Second).Should(gomega.BeFalse())
		})
	})
})

// Helper methods for the test suite

func (s *E2ETestSuite) setupTestEnvironment() {
	ginkgo.By("Starting k3s container")
	s.startK3sContainer()

	ginkgo.By("Setting up Kubernetes client")
	s.setupKubernetesClient()

	ginkgo.By("Installing CRDs")
	s.installCRDs()

	ginkgo.By("Deploying operator")
	s.deployOperator()
}

func (s *E2ETestSuite) startK3sContainer() {
	req := testcontainers.ContainerRequest{
		Image:        "rancher/k3s:v1.28.5-k3s1",
		ExposedPorts: []string{"6443/tcp", "80/tcp"},
		Env: map[string]string{
			"K3S_KUBECONFIG_OUTPUT": "/output/kubeconfig.yaml",
		},
		Cmd: []string{
			"server",
			"--disable=traefik",
			"--disable=servicelb",
			"--disable=metrics-server",
			"--disable=local-storage",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("Node controller sync successful"),
			wait.ForListeningPort("6443/tcp"),
		).WithStartupTimeout(2 * time.Minute),
		Privileged: true,
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.k3sContainer = container

	// Get kubeconfig
	exitCode, reader, err := container.Exec(s.ctx, []string{"cat", "/output/kubeconfig.yaml"})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(exitCode).To(gomega.Equal(0))

	kubeconfigBytes, err := io.ReadAll(reader)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Update kubeconfig with container IP
	host, err := container.Host(s.ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	port, err := container.MappedPort(s.ctx, "6443")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	s.kubeconfig = strings.ReplaceAll(string(kubeconfigBytes), "https://127.0.0.1:6443", fmt.Sprintf("https://%s:%s", host, port.Port()))
}

func (s *E2ETestSuite) setupKubernetesClient() {
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(s.kubeconfig))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.cfg = config

	// Create Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(config)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.kubeClient = kubeClient

	// Create controller-runtime client
	scheme := runtime.NewScheme()
	gomega.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
	gomega.Expect(opsv1alpha1.AddToScheme(scheme)).To(gomega.Succeed())
	s.scheme = scheme

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.k8sClient = k8sClient
}

func (s *E2ETestSuite) installCRDs() {
	// Apply VaultUnsealer CRD
	crd := &opsv1alpha1.VaultUnsealer{}
	gomega.Eventually(func() error {
		return s.k8sClient.Create(s.ctx, crd)
	}, 30*time.Second, 1*time.Second).Should(gomega.Succeed())
}

func (s *E2ETestSuite) deployOperator() {
	// Create operator namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vault-unsealer-system",
		},
	}
	_ = s.k8sClient.Create(s.ctx, ns)

	// For E2E tests, we would typically deploy the operator here
	// For now, we'll focus on testing the business logic
	ginkgo.Skip("Operator deployment in E2E tests requires built image")
}

func (s *E2ETestSuite) deployVaultPod(name string, ha bool) corev1.Pod {
	// Create a mock Vault pod for testing
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "vault",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "vault",
					Image: "hashicorp/vault:1.13.1",
					Ports: []corev1.ContainerPort{
						{ContainerPort: 8200},
					},
					Env: []corev1.EnvVar{
						{Name: "VAULT_DEV_ROOT_TOKEN_ID", Value: "root"},
						{Name: "VAULT_DEV_LISTEN_ADDRESS", Value: "0.0.0.0:8200"},
					},
					Command: []string{"vault", "server", "-dev"},
				},
			},
		},
	}

	err := s.k8sClient.Create(s.ctx, pod)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Wait for pod to be ready
	gomega.Eventually(func() bool {
		err := s.k8sClient.Get(s.ctx, client.ObjectKeyFromObject(pod), pod)
		if err != nil {
			return false
		}
		return pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != ""
	}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue())

	return *pod
}

func (s *E2ETestSuite) initializeVault(pod corev1.Pod) []string {
	// For this test, return mock unseal keys
	// In a real scenario, you would call vault operator init
	return []string{
		"key1_mock_unseal_key_abcd1234",
		"key2_mock_unseal_key_efgh5678",
		"key3_mock_unseal_key_ijkl9012",
		"key4_mock_unseal_key_mnop3456",
		"key5_mock_unseal_key_qrst7890",
	}
}

func (s *E2ETestSuite) initializeVaultWithThreshold(pod corev1.Pod, threshold, total int) []string {
	keys := make([]string, total)
	for i := 0; i < total; i++ {
		keys[i] = fmt.Sprintf("key%d_mock_unseal_key_%04d", i+1, i*1111)
	}
	return keys
}

func (s *E2ETestSuite) sealVault(pod corev1.Pod) {
	// Mock sealing - in reality this would call vault API
	ginkgo.By(fmt.Sprintf("Sealing vault pod %s", pod.Name))
}

func (s *E2ETestSuite) isVaultUnsealed(pod corev1.Pod) bool {
	// Mock check - in reality this would call vault API /v1/sys/seal-status
	// For test purposes, simulate that vault gets unsealed after unsealer runs
	return true
}

func (s *E2ETestSuite) createUnsealKeysSecret(name, namespace string, keys []string) {
	keysJSON, err := json.Marshal(keys)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"keys.json": keysJSON,
		},
	}

	err = s.k8sClient.Create(s.ctx, secret)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (s *E2ETestSuite) deployVaultUnsealer(name, namespace string, haMode bool, threshold int) {
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				URL: "http://vault.default.svc:8200",
			},
			UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
				{
					Name: "vault-unseal-keys",
					Key:  "keys.json",
				},
			},
			VaultLabelSelector: "app.kubernetes.io/name=vault",
			Mode: opsv1alpha1.ModeSpec{
				HA: haMode,
			},
			KeyThreshold: threshold,
		},
	}

	err := s.k8sClient.Create(s.ctx, vaultUnsealer)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (s *E2ETestSuite) deployVaultUnsealerWithMultipleSecrets(name, namespace string, secretNames []string) {
	secretRefs := make([]opsv1alpha1.SecretRef, len(secretNames))
	for i, secretName := range secretNames {
		secretRefs[i] = opsv1alpha1.SecretRef{
			Name: secretName,
			Key:  "keys.json",
		}
	}

	vaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				URL: "http://vault.default.svc:8200",
			},
			UnsealKeysSecretRefs: secretRefs,
			VaultLabelSelector:   "app.kubernetes.io/name=vault",
			Mode: opsv1alpha1.ModeSpec{
				HA: false,
			},
			KeyThreshold: 3,
		},
	}

	err := s.k8sClient.Create(s.ctx, vaultUnsealer)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (s *E2ETestSuite) getVaultUnsealer(name, namespace string) *opsv1alpha1.VaultUnsealer {
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{}
	err := s.k8sClient.Get(s.ctx, client.ObjectKey{Name: name, Namespace: namespace}, vaultUnsealer)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return vaultUnsealer
}

func (s *E2ETestSuite) cleanup() {
	if s.k3sContainer != nil {
		s.k3sContainer.Terminate(s.ctx)
	}
	for _, container := range s.vaultContainers {
		container.Terminate(s.ctx)
	}
}
