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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

var _ = Describe("VaultUnsealer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vaultunsealer := &opsv1alpha1.VaultUnsealer{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VaultUnsealer")
			err := k8sClient.Get(ctx, typeNamespacedName, vaultunsealer)
			if err != nil && errors.IsNotFound(err) {
				resource := &opsv1alpha1.VaultUnsealer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: opsv1alpha1.VaultUnsealerSpec{
						Vault: opsv1alpha1.VaultConnectionSpec{
							URL: "https://vault.test.svc:8200",
						},
						UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
							{
								Name: "test-unseal-keys",
								Key:  "keys.json",
							},
						},
						VaultLabelSelector: "app.kubernetes.io/name=vault",
						Mode: opsv1alpha1.ModeSpec{
							HA: true,
						},
						KeyThreshold: 3,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &opsv1alpha1.VaultUnsealer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VaultUnsealer")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VaultUnsealerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
