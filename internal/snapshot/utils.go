package snapshot

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func clearWorkdir(workdir string) error {
	entries, err := os.ReadDir(workdir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// 清理时同样需要保护元数据目录不被删除
		if isRewindMetaPath(entry.Name()) {
			continue
		}
		if err = os.RemoveAll(filepath.Join(workdir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(srcRoot, dstRoot string, skip func(rel string) bool) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstRoot, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(path)
			if e != nil {
				return e
			}
			if e = os.MkdirAll(filepath.Dir(dst), 0o755); e != nil {
				return e
			}
			return os.Symlink(link, dst)
		}
		if d.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		
		// 优先尝试硬链接以节省存储空间
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Link(path, dst); err == nil {
			return nil
		}
		
		// 硬链接失败（比如跨挂载点/文件系统时），回退到物理拷贝
		return copyFile(path, dst, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}