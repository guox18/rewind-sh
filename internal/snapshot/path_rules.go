package snapshot

import (
	"path/filepath"
	"strings"
)

func isRewindMetaPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	// 兼容旧版 .rewind 和新版 .rewind-sh，防止在监控 Home 目录时发生无限递归拷贝
	if rel == ".rewind" || strings.HasPrefix(rel, ".rewind/") {
		return true
	}
	if rel == ".rewind-sh" || strings.HasPrefix(rel, ".rewind-sh/") {
		return true
	}
	return false
}
