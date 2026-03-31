package naming

import "strings"

// ClusterMatchesEnv reports whether a cluster name ends with -{env}.
func ClusterMatchesEnv(cluster, env string) bool {
	return strings.HasSuffix(cluster, "-"+env)
}

// AppGroup extracts the application group from a cluster name by stripping the
// environment suffix (e.g. "home-staging" → "home").
func AppGroup(cluster, env string) string {
	return strings.TrimSuffix(cluster, "-"+env)
}

// ServiceMatchesConvention checks if a service name follows either
// {appGroup}-{env} or {appGroup}-{slug}-{env}.
func ServiceMatchesConvention(service, appGroup, env string) bool {
	if service == appGroup+"-"+env {
		return true
	}
	prefix := appGroup + "-"
	suffix := "-" + env
	return strings.HasPrefix(service, prefix) &&
		strings.HasSuffix(service, suffix) &&
		len(service) > len(prefix)+len(suffix)
}

// ServiceToSlug converts a full service name to a human-friendly slug.
// {appGroup}-{env} → "web", {appGroup}-{slug}-{env} → slug.
func ServiceToSlug(service, appGroup, env string) string {
	if service == appGroup+"-"+env {
		return "web"
	}
	return strings.TrimPrefix(strings.TrimSuffix(service, "-"+env), appGroup+"-")
}

// SlugToServiceName converts a slug back to the full ECS service name.
func SlugToServiceName(slug, appGroup, env string) string {
	if slug == "web" {
		return appGroup + "-" + env
	}
	return appGroup + "-" + slug + "-" + env
}
