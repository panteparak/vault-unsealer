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
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

type Loader struct {
	client client.Client
}

func NewLoader(client client.Client) *Loader {
	return &Loader{client: client}
}

func (l *Loader) LoadUnsealKeys(ctx context.Context, namespace string, secretRefs []opsv1alpha1.SecretRef, keyThreshold int) ([]string, error) {
	var allKeys []string
	keySet := make(map[string]bool)

	for _, secretRef := range secretRefs {
		keys, err := l.loadKeysFromSecret(ctx, namespace, secretRef)
		if err != nil {
			return nil, fmt.Errorf("failed to load keys from secret %s/%s: %w", secretRef.Namespace, secretRef.Name, err)
		}

		for _, key := range keys {
			if !keySet[key] {
				keySet[key] = true
				allKeys = append(allKeys, key)
			}
		}
	}

	if len(allKeys) == 0 {
		return nil, fmt.Errorf("no unseal keys found in any referenced secrets")
	}

	if keyThreshold > 0 && len(allKeys) > keyThreshold {
		allKeys = allKeys[:keyThreshold]
	}

	return allKeys, nil
}

func (l *Loader) loadKeysFromSecret(ctx context.Context, defaultNamespace string, secretRef opsv1alpha1.SecretRef) ([]string, error) {
	namespace := secretRef.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	secret := &corev1.Secret{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretRef.Name,
	}

	if err := l.client.Get(ctx, namespacedName, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	data, ok := secret.Data[secretRef.Key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in secret", secretRef.Key)
	}

	return l.parseKeys(string(data))
}

func (l *Loader) parseKeys(data string) ([]string, error) {
	data = strings.TrimSpace(data)

	if strings.HasPrefix(data, "[") && strings.HasSuffix(data, "]") {
		var keys []string
		if err := json.Unmarshal([]byte(data), &keys); err != nil {
			return nil, fmt.Errorf("failed to parse JSON array: %w", err)
		}
		return keys, nil
	}

	lines := strings.Split(data, "\n")
	var keys []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			keys = append(keys, line)
		}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no keys found in secret data")
	}

	return keys, nil
}
