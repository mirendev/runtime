//go:build darwin

package sysstats

// CollectSystemStats returns empty stats on Darwin (stub implementation)
func CollectSystemStats(dataPath string) ResourceUsage {
	return ResourceUsage{}
}
