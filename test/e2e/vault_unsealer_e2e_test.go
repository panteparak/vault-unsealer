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

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
	"github.com/panteparak/vault-unsealer/internal/secrets"
)

type VaultE2ETestSuite struct {
	k3sContainer testcontainers.Container
	ctx          context.Context
	k8sClient    client.Client
	kubeClient   kubernetes.Interface
	cfg          *rest.Config
}

var _ = ginkgo.Describe("Vault Unsealer E2E with K3s", func() {
	var suite *VaultE2ETestSuite

	ginkgo.BeforeEach(func() {
		suite = &VaultE2ETestSuite{
			ctx: context.Background(),
		}
		suite.setupK3sEnvironment()
	})

	ginkgo.AfterEach(func() {
		if suite.k3sContainer != nil {
			suite.k3sContainer.Terminate(suite.ctx)
		}
	})

	ginkgo.Context("Secrets Loading Tests", func() {
		ginkgo.It("should load and deduplicate keys from multiple secrets", func() {
			// Create test secrets with different formats
			suite.createTestSecrets()

			// Test the secrets loader directly
			loader := secrets.NewLoader(suite.k8sClient)

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "vault-keys-json", Key: "keys.json"},
				{Name: "vault-keys-text", Key: "keys.txt"},
				{Name: "vault-keys-mixed", Key: "mixed.json"},
			}

			keys, err := loader.LoadUnsealKeys(suite.ctx, "default", secretRefs, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Should have deduplicated keys
			gomega.Expect(len(keys)).To(gomega.BeNumerically(">", 0))
			gomega.Expect(len(keys)).To(gomega.BeNumerically("<=", 7)) // Max unique keys from test data

			// Verify specific keys are loaded
			gomega.Expect(keys).To(gomega.ContainElement("key1"))
			gomega.Expect(keys).To(gomega.ContainElement("key2"))
			gomega.Expect(keys).To(gomega.ContainElement("key3"))
		})

		ginkgo.It("should respect key threshold", func() {
			suite.createTestSecrets()

			loader := secrets.NewLoader(suite.k8sClient)

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "vault-keys-json", Key: "keys.json"},
			}

			// Test with threshold of 2
			keys, err := loader.LoadUnsealKeys(suite.ctx, "default", secretRefs, 2)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(keys)).To(gomega.Equal(2))
		})

		ginkgo.It("should handle cross-namespace secrets", func() {
			// Create a secret in a different namespace
			suite.createNamespace("vault-ns")
			suite.createSecretInNamespace("vault-ns", "cross-ns-secret", map[string][]byte{
				"keys.json": []byte(`["cross-ns-key1", "cross-ns-key2"]`),
			})

			loader := secrets.NewLoader(suite.k8sClient)

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "cross-ns-secret", Namespace: "vault-ns", Key: "keys.json"},
			}

			keys, err := loader.LoadUnsealKeys(suite.ctx, "default", secretRefs, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.ContainElement("cross-ns-key1"))
			gomega.Expect(keys).To(gomega.ContainElement("cross-ns-key2"))
		})

		ginkgo.It("should handle missing secrets gracefully", func() {
			loader := secrets.NewLoader(suite.k8sClient)

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "non-existent-secret", Key: "keys.json"},
			}

			_, err := loader.LoadUnsealKeys(suite.ctx, "default", secretRefs, 0)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to get secret"))
		})
	})

	ginkgo.Context("VaultUnsealer Resource Tests", func() {
		ginkgo.It("should create and validate VaultUnsealer resources", func() {
			// Create test secrets first
			suite.createTestSecrets()

			// Create VaultUnsealer resource
			vaultUnsealer := &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vault-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "http://vault.default.svc:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{Name: "vault-keys-json", Key: "keys.json"},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3,
				},
			}

			err := suite.k8sClient.Create(suite.ctx, vaultUnsealer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify the resource was created
			createdUnsealer := &opsv1alpha1.VaultUnsealer{}
			err = suite.k8sClient.Get(suite.ctx, client.ObjectKeyFromObject(vaultUnsealer), createdUnsealer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify spec fields
			gomega.Expect(createdUnsealer.Spec.Vault.URL).To(gomega.Equal("http://vault.default.svc:8200"))
			gomega.Expect(createdUnsealer.Spec.Mode.HA).To(gomega.BeTrue())
			gomega.Expect(createdUnsealer.Spec.KeyThreshold).To(gomega.Equal(3))
		})

		ginkgo.It("should update VaultUnsealer status", func() {
			// Create VaultUnsealer resource
			vaultUnsealer := &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-status-update",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "http://vault.default.svc:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{Name: "vault-keys-json", Key: "keys.json"},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode:               opsv1alpha1.ModeSpec{HA: false},
					KeyThreshold:       3,
				},
			}

			err := suite.k8sClient.Create(suite.ctx, vaultUnsealer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Update status
			now := metav1.Now()
			vaultUnsealer.Status = opsv1alpha1.VaultUnsealerStatus{
				PodsChecked:       []string{"vault-0", "vault-1"},
				UnsealedPods:      []string{"vault-0"},
				LastReconcileTime: &now,
				Conditions: []opsv1alpha1.Condition{
					{
						Type:    "Ready",
						Status:  "True",
						Reason:  "ReconcileSuccess",
						Message: "Successfully unsealed 1 pods",
					},
				},
			}

			err = suite.k8sClient.Status().Update(suite.ctx, vaultUnsealer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify status update
			updatedUnsealer := &opsv1alpha1.VaultUnsealer{}
			err = suite.k8sClient.Get(suite.ctx, client.ObjectKeyFromObject(vaultUnsealer), updatedUnsealer)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(updatedUnsealer.Status.PodsChecked).To(gomega.Equal([]string{"vault-0", "vault-1"}))
			gomega.Expect(updatedUnsealer.Status.UnsealedPods).To(gomega.Equal([]string{"vault-0"}))
			gomega.Expect(len(updatedUnsealer.Status.Conditions)).To(gomega.Equal(1))
			gomega.Expect(updatedUnsealer.Status.Conditions[0].Type).To(gomega.Equal("Ready"))
		})
	})
})

func (s *VaultE2ETestSuite) setupK3sEnvironment() {
	ginkgo.By("Starting k3s container")

	req := testcontainers.ContainerRequest{
		Image:        "rancher/k3s:v1.28.5-k3s1",
		ExposedPorts: []string{"6443/tcp"},
		Env: map[string]string{
			"K3S_KUBECONFIG_OUTPUT": "/output/kubeconfig.yaml",
			"K3S_KUBECONFIG_MODE":   "666",
		},
		Cmd: []string{
			"server",
			"--disable=traefik",
			"--disable=servicelb",
			"--disable=metrics-server",
			"--disable=local-storage",
			"--write-kubeconfig-mode=666",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("Node controller sync successful").WithStartupTimeout(2*time.Minute),
			wait.ForListeningPort("6443/tcp"),
		),
		Privileged: true,
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.k3sContainer = container

	ginkgo.By("Setting up Kubernetes client")
	s.setupKubernetesClient()
}

func (s *VaultE2ETestSuite) setupKubernetesClient() {
	// Get kubeconfig from container
	exitCode, reader, err := s.k3sContainer.Exec(s.ctx, []string{"cat", "/output/kubeconfig.yaml"})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(exitCode).To(gomega.Equal(0))

	kubeconfigBytes, err := io.ReadAll(reader)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Get container connection details
	host, err := s.k3sContainer.Host(s.ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	port, err := s.k3sContainer.MappedPort(s.ctx, "6443")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Replace localhost with actual container host
	kubeconfig := strings.ReplaceAll(string(kubeconfigBytes), "https://127.0.0.1:6443", fmt.Sprintf("https://%s:%s", host, port.Port()))

	// Create rest config
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.cfg = config

	// Create Kubernetes clientset
	kubeClient, err := kubernetes.NewForConfig(config)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.kubeClient = kubeClient

	// Create controller-runtime client
	scheme := runtime.NewScheme()
	gomega.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
	gomega.Expect(opsv1alpha1.AddToScheme(scheme)).To(gomega.Succeed())

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s.k8sClient = k8sClient

	// Wait for API server to be ready
	gomega.Eventually(func() error {
		_, err := s.kubeClient.CoreV1().Namespaces().List(s.ctx, metav1.ListOptions{})
		return err
	}, 30*time.Second, 2*time.Second).Should(gomega.Succeed())
}

func (s *VaultE2ETestSuite) createTestSecrets() {
	// JSON format secret
	s.createSecret("vault-keys-json", map[string][]byte{
		"keys.json": []byte(`["key1", "key2", "key3"]`),
	})

	// Text format secret
	s.createSecret("vault-keys-text", map[string][]byte{
		"keys.txt": []byte("key4\nkey5\nkey6"),
	})

	// Mixed format with overlapping keys for deduplication test
	s.createSecret("vault-keys-mixed", map[string][]byte{
		"mixed.json": []byte(`["key2", "key7"]`), // key2 overlaps for deduplication
	})
}

func (s *VaultE2ETestSuite) createSecret(name string, data map[string][]byte) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	err := s.k8sClient.Create(s.ctx, secret)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (s *VaultE2ETestSuite) createNamespace(name string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	err := s.k8sClient.Create(s.ctx, ns)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (s *VaultE2ETestSuite) createSecretInNamespace(namespace, name string, data map[string][]byte) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	err := s.k8sClient.Create(s.ctx, secret)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}
