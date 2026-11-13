package ansible

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CollectLocalRepoCopy lists the files a copy of the clone at dir carries: what the editor shows (tracked and untracked,
// not ignored; files deleted from the working tree are left out) and, with includeGit, the whole .git directory.
// Only regular files are copied. Symlinks (and submodule folders) are skipped and counted: extracted on a host, a link could
// point outside the destination and a later archive member could then be written through it. total is the archive size
// (headers and padding included, roughly) for progress.
func CollectLocalRepoCopy(ctx context.Context, dir string, includeGit bool) (files []RepoFile, total int64, skipped int, err error) {
	l, err := openLocalGit(dir, nil, "https:ssh")
	if err != nil {
		return nil, 0, 0, err
	}
	defer l.close()
	out, err := l.run(ctx, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, 0, 0, err
	}
	names := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	sort.Strings(names)
	prev := ""
	add := func(rel string, fi fs.FileInfo) {
		files = append(files, RepoFile{Path: rel, Size: fi.Size()})
		total += (fi.Size()+511)/512*512 + 512
	}
	for _, n := range names {
		if n == "" || n == prev {
			continue
		}
		prev = n
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(n)))
		switch {
		case err != nil: // tracked but deleted in the working tree
		case fi.Mode().IsRegular():
			add(n, fi)
		default:
			skipped++
		}
	}
	if includeGit {
		root := filepath.Join(dir, ".git")
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			switch {
			case d.Name() == "index.lock": // transient lock of a running git command
			case fi.Mode().IsRegular():
				add(filepath.ToSlash(rel), fi)
			default:
				skipped++
			}
			return nil
		})
		if err != nil {
			return nil, 0, 0, err
		}
	}
	return files, total, skipped, nil
}

// WriteLocalRepoTar streams files (from CollectLocalRepoCopy) as a tar archive for `tar -x` on a host: relative paths,
// permission bits and modification times kept, owner left to the extracting user. A file that vanished or shrank since it
// was collected aborts the copy instead of sending a damaged archive.
func WriteLocalRepoTar(ctx context.Context, w io.Writer, dir string, files []RepoFile) error {
	tw := tar.NewWriter(w)
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(dir, filepath.FromSlash(f.Path))
		fi, err := os.Lstat(full)
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("%s changed while copying", f.Path)
		}
		src, err := os.Open(full)
		if err != nil {
			return err
		}
		hdr := &tar.Header{Typeflag: tar.TypeReg, Name: f.Path, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			src.Close()
			return err
		}
		_, err = io.CopyN(tw, src, hdr.Size)
		src.Close()
		if err != nil {
			return fmt.Errorf("%s changed while copying: %w", f.Path, err)
		}
	}
	return tw.Close()
}
