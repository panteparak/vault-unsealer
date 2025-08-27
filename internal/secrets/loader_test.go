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

package secrets

import (
	"context"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

var _ = ginkgo.Describe("Secrets Loader", func() {
	var (
		ctx       context.Context
		k8sClient client.Client
		loader    *Loader
		scheme    *runtime.Scheme
	)

	ginkgo.BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		gomega.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		loader = NewLoader(k8sClient)
	})

	ginkgo.Context("parseKeys", func() {
		ginkgo.It("should parse JSON array format", func() {
			data := `["key1", "key2", "key3"]`
			keys, err := loader.parseKeys(data)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.Equal([]string{"key1", "key2", "key3"}))
		})

		ginkgo.It("should parse newline-separated format", func() {
			data := "key1\nkey2\nkey3"
			keys, err := loader.parseKeys(data)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.Equal([]string{"key1", "key2", "key3"}))
		})

		ginkgo.It("should handle empty lines in newline format", func() {
			data := "key1\n\nkey2\n\nkey3\n"
			keys, err := loader.parseKeys(data)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.Equal([]string{"key1", "key2", "key3"}))
		})

		ginkgo.It("should treat invalid JSON as newline format", func() {
			data := `["key1", "key2"`
			keys, err := loader.parseKeys(data)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.Equal([]string{`["key1", "key2"`}))
		})

		ginkgo.It("should return error for empty data", func() {
			data := ""
			_, err := loader.parseKeys(data)
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("LoadUnsealKeys", func() {
		ginkgo.It("should load keys from multiple secrets", func() {
			// Create test secrets
			secret1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret1",
					Namespace: "test",
				},
				Data: map[string][]byte{
					"keys": []byte(`["key1", "key2"]`),
				},
			}
			secret2 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret2",
					Namespace: "test",
				},
				Data: map[string][]byte{
					"keys": []byte("key3\nkey4"),
				},
			}

			gomega.Expect(k8sClient.Create(ctx, secret1)).To(gomega.Succeed())
			gomega.Expect(k8sClient.Create(ctx, secret2)).To(gomega.Succeed())

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "secret1", Key: "keys"},
				{Name: "secret2", Key: "keys"},
			}

			keys, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.ConsistOf("key1", "key2", "key3", "key4"))
		})

		ginkgo.It("should deduplicate keys", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret",
					Namespace: "test",
				},
				Data: map[string][]byte{
					"keys1": []byte(`["key1", "key2"]`),
					"keys2": []byte("key2\nkey3"),
				},
			}

			gomega.Expect(k8sClient.Create(ctx, secret)).To(gomega.Succeed())

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "secret", Key: "keys1"},
				{Name: "secret", Key: "keys2"},
			}

			keys, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.ConsistOf("key1", "key2", "key3"))
		})

		ginkgo.It("should respect key threshold", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret",
					Namespace: "test",
				},
				Data: map[string][]byte{
					"keys": []byte(`["key1", "key2", "key3", "key4", "key5"]`),
				},
			}

			gomega.Expect(k8sClient.Create(ctx, secret)).To(gomega.Succeed())

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "secret", Key: "keys"},
			}

			keys, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 3)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(keys)).To(gomega.Equal(3))
			// Verify all returned keys are from the original set
			allKeys := []string{"key1", "key2", "key3", "key4", "key5"}
			for _, key := range keys {
				gomega.Expect(allKeys).To(gomega.ContainElement(key))
			}
		})

		ginkgo.It("should handle cross-namespace secrets", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret",
					Namespace: "other-namespace",
				},
				Data: map[string][]byte{
					"keys": []byte(`["key1", "key2"]`),
				},
			}

			gomega.Expect(k8sClient.Create(ctx, secret)).To(gomega.Succeed())

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "secret", Namespace: "other-namespace", Key: "keys"},
			}

			keys, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(keys).To(gomega.Equal([]string{"key1", "key2"}))
		})

		ginkgo.It("should return error for missing secret", func() {
			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "nonexistent", Key: "keys"},
			}

			_, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 0)
			gomega.Expect(err).To(gomega.HaveOccurred())
		})

		ginkgo.It("should return error for missing key in secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret",
					Namespace: "test",
				},
				Data: map[string][]byte{
					"other-key": []byte(`["key1", "key2"]`),
				},
			}

			gomega.Expect(k8sClient.Create(ctx, secret)).To(gomega.Succeed())

			secretRefs := []opsv1alpha1.SecretRef{
				{Name: "secret", Key: "keys"},
			}

			_, err := loader.LoadUnsealKeys(ctx, "test", secretRefs, 0)
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})
})

func TestSecretsLoader(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Secrets Loader Suite")
}
