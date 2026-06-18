package pipeline

import "strings"

func SanitizeDetail(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	sanitized := make(map[string]interface{}, len(value))
	for key, item := range value {
		sanitized[key] = sanitizeValue(key, item)
	}
	return sanitized
}

func sanitizeValue(key string, value interface{}) interface{} {
	if sensitiveKey(key) {
		return "******"
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return SanitizeDetail(typed)
	case []interface{}:
		items := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			items = append(items, sanitizeValue("", item))
		}
		return items
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "token", "secret", "credential", "kubeconfig", "private_key", "apikey", "api_key"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
