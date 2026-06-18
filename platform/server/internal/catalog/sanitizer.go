package catalog

import "strings"

func SanitizeCatalog(input Catalog) Catalog {
	output := input
	for i := range output.Services {
		for j := range output.Services[i].Environments {
			env := &output.Services[i].Environments[j]
			env.Git.Repo = sanitizeURL(env.Git.Repo)
		}
	}
	return output
}

func sanitizeURL(value string) string {
	if value == "" {
		return value
	}
	if !strings.Contains(value, "@") {
		return value
	}
	parts := strings.SplitN(value, "@", 2)
	if strings.Contains(parts[0], "://") {
		scheme := strings.SplitN(parts[0], "://", 2)[0]
		return scheme + "://******@" + parts[1]
	}
	return "******@" + parts[1]
}
