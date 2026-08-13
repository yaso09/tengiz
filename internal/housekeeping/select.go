package housekeeping

import (
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/runtime"
)

const tengizAppLabel = "tengiz-app"

func selectHelperContainers(all []runtime.ContainerInfo) []string {
	var ids []string
	for _, c := range all {
		if c.State == "running" {
			continue
		}
		if _, ok := c.Labels[tengizAppLabel]; ok {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

func selectDanglingImages(imgs []runtime.ImageInfo) []string {
	var ids []string
	for _, img := range imgs {
		if img.Tag == "<none>:<none>" && !img.InUse {
			ids = append(ids, img.ID)
		}
	}
	return ids
}

func selectOldAppImages(imgs []runtime.ImageInfo, keep int) []string {
	if keep <= 0 {
		keep = 5
	}
	byRepo := make(map[string][]runtime.ImageInfo)
	for _, img := range imgs {
		if img.Tag == "<none>:<none>" || img.InUse {
			continue
		}
		if strings.HasSuffix(img.Tag, ":latest") {
			continue
		}
		repo := strings.SplitN(img.Tag, ":", 2)[0]
		if !strings.HasPrefix(repo, "tengiz-apps/") {
			continue
		}
		byRepo[repo] = append(byRepo[repo], img)
	}
	var toRemove []string
	for _, appImgs := range byRepo {
		sort.Slice(appImgs, func(i, j int) bool {
			return appImgs[i].CreatedAt < appImgs[j].CreatedAt
		})
		if len(appImgs) <= keep {
			continue
		}
		for i := 0; i < len(appImgs)-keep; i++ {
			toRemove = append(toRemove, appImgs[i].Tag)
		}
	}
	sort.Strings(toRemove)
	return toRemove
}
