package runtime

import (
	"sort"
	"strings"
)

func parseContainerRow(line string) (id, state, labels string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if kv[0] == key {
			return true
		}
	}
	return false
}

func selectContainersToRemove(lines []string, all bool) []string {
	var ids []string
	for _, line := range lines {
		id, state, labels := parseContainerRow(line)
		if id == "" {
			continue
		}
		if state == "running" {
			continue
		}
		if !all && hasLabel(labels, labelKey) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func parseImageRow(line string) (repoTag, id, createdAt string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func selectImagesToRemove(lines, usedTags []string, protectedApps []string, keepN int, all bool) []string {
	used := make(map[string]bool, len(usedTags))
	for _, t := range usedTags {
		used[t] = true
	}

	createdAt := make(map[string]string)
	byApp := make(map[string][]string)
	var toRemove []string

	for _, line := range lines {
		repoTag, id, created := parseImageRow(line)
		if repoTag == "" {
			continue
		}
		if strings.HasPrefix(repoTag, "<none>:") {
			toRemove = append(toRemove, id)
			continue
		}
		if used[repoTag] {
			continue
		}
		idx := strings.LastIndex(repoTag, ":")
		if idx < 0 {
			continue
		}
		repo, tag := repoTag[:idx], repoTag[idx+1:]
		if strings.HasPrefix(repo, "tengiz-apps/") {
			if strings.HasSuffix(tag, "-latest") {
				continue
			}
			createdAt[repoTag] = created
			byApp[repo] = append(byApp[repo], repoTag)
			continue
		}
		if all {
			toRemove = append(toRemove, repoTag)
		}
	}

	for repo, tags := range byApp {
		appName := strings.TrimPrefix(repo, "tengiz-apps/")
		if !containsString(protectedApps, appName) {
			toRemove = append(toRemove, tags...)
			continue
		}
		sort.Slice(tags, func(i, j int) bool {
			return createdAt[tags[i]] > createdAt[tags[j]]
		})
		for i := keepN; i < len(tags); i++ {
			toRemove = append(toRemove, tags[i])
		}
	}
	return toRemove
}

func extractTotalSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return strings.TrimSpace(output)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
