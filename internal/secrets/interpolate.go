package secrets

import (
	"regexp"
)

var secretPattern = regexp.MustCompile(`\[\[secret\.([a-zA-Z_][a-zA-Z0-9_]*)\]\]`)

func ResolveInterpolations(env map[string]string, appSecrets map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = secretPattern.ReplaceAllStringFunc(v, func(match string) string {
			matches := secretPattern.FindStringSubmatch(match)
			if len(matches) < 2 {
				return match
			}
			secretKey := matches[1]
			if val, ok := appSecrets[secretKey]; ok {
				return val
			}
			return match
		})
	}
	return result
}
