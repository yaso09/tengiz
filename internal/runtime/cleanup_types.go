package runtime

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
}

type CleanupReport struct {
	ContainersRemoved int
	ContainersFreed   string
	ImagesRemoved     int
	ImagesFreed       string
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheFreed   string
	Errors            []string
}
