package housekeeping

import "context"

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryNetworks   Category = "networks"
	CategoryCache      Category = "cache"
	CategoryVolumes    Category = "volumes"
)

var DefaultCategories = []Category{
	CategoryContainers,
	CategoryImages,
	CategoryNetworks,
	CategoryCache,
}

type Options struct {
	Categories []Category
	Apply      bool
}

type Usage struct {
	ContainersReclaimable int64
	ImagesReclaimable     int64
	VolumesReclaimable    int64
	CacheReclaimable      int64
}

type Candidate struct {
	Category Category
	ID       string
	Name     string
}

type PruneResult struct {
	Applied             bool
	Candidates          []Candidate
	ReclaimedBytes      int64
	ReclaimedByCategory map[Category]int64
}

type Manager interface {
	DiskUsage(ctx context.Context) (*Usage, error)
	Prune(ctx context.Context, opts Options) (*PruneResult, error)
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) DiskUsage(ctx context.Context) (*Usage, error) {
	return &Usage{}, nil
}

func (m *stubManager) Prune(ctx context.Context, opts Options) (*PruneResult, error) {
	return &PruneResult{Applied: opts.Apply}, nil
}