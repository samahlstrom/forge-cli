package util

import (
	"gopkg.in/yaml.v3"
)

// ReadYAML reads and parses a YAML file.
func ReadYAML(path string, v any) error {
	data, err := ReadText(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal([]byte(data), v)
}

// WriteYAML writes a value as YAML to a file.
func WriteYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return WriteText(path, string(data))
}
