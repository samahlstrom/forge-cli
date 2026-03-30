package render

import (
	"gopkg.in/yaml.v3"
)

// MergeForgeYAML merges new forge.yaml fields into existing config
// without overwriting user values. Only adds keys that don't exist.
func MergeForgeYAML(existing, updated string) (string, error) {
	var existingDoc, updatedDoc map[string]any
	if err := yaml.Unmarshal([]byte(existing), &existingDoc); err != nil {
		return existing, err
	}
	if err := yaml.Unmarshal([]byte(updated), &updatedDoc); err != nil {
		return existing, err
	}

	merged := deepMergeNewOnly(existingDoc, updatedDoc)
	out, err := yaml.Marshal(merged)
	if err != nil {
		return existing, err
	}
	return string(out), nil
}

func deepMergeNewOnly(target, source map[string]any) map[string]any {
	result := make(map[string]any, len(target))
	for k, v := range target {
		result[k] = v
	}
	for k, v := range source {
		existing, exists := result[k]
		if !exists {
			result[k] = v
			continue
		}
		// Both are maps → recurse
		em, eOk := existing.(map[string]any)
		sm, sOk := v.(map[string]any)
		if eOk && sOk {
			result[k] = deepMergeNewOnly(em, sm)
		}
		// Otherwise keep existing value
	}
	return result
}
