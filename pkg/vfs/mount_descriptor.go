package vfs

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const mountDescriptorSuffix = ".mount.toml"

func MountDescriptorPath(root string, mountPath string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	normalized, err := ValidateMountPath(mountPath)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(normalized, "/")
	dir := path.Dir(rel)
	name := path.Base(rel)
	if dir == "." {
		dir = ""
	}
	return filepath.Join(rootAbs, filepath.FromSlash(dir), name+mountDescriptorSuffix), nil
}

func WriteMountDescriptorFile(root string, mount Mount) (Mount, error) {
	filename, err := MountDescriptorPath(root, mount.MountPath)
	if err != nil {
		return Mount{}, err
	}
	normalized, err := ValidateMountPath(mount.MountPath)
	if err != nil {
		return Mount{}, err
	}
	if strings.TrimSpace(mount.Source) == "" {
		return Mount{}, &Error{Code: "validation_failed", Message: "Mount source is required"}
	}
	classification, err := ClassifySource(mount.Source)
	if err != nil {
		return Mount{}, err
	}
	mount.MountPath = normalized
	mount.RegistryPath = filename
	mount.Kind = classification.Kind
	if mount.Kind == "local" {
		source := mount.Source
		if !filepath.IsAbs(source) {
			source = filepath.Join(filepath.Dir(filename), source)
		}
		abs, err := filepath.Abs(source)
		if err != nil {
			return Mount{}, err
		}
		mount.SourcePath = filepath.Clean(abs)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return Mount{}, err
	}
	if err := os.WriteFile(filename, []byte(formatMountDescriptor(mount)), 0o644); err != nil {
		return Mount{}, err
	}
	return mount, nil
}

func formatMountDescriptor(mount Mount) string {
	var b strings.Builder
	writeMountDescriptorString(&b, "source", mount.Source)
	if mount.Writable {
		b.WriteString("writable = true\n")
	} else {
		b.WriteString("writable = false\n")
	}
	writeOptionalMountDescriptorString(&b, "title", mount.Title)
	writeOptionalMountDescriptorString(&b, "description", mount.Description)
	writeOptionalMountDescriptorString(&b, "when_to_use", mount.WhenToUse)
	writeOptionalMountDescriptorString(&b, "when_not_to_use", mount.WhenNotToUse)
	writeOptionalMountDescriptorString(&b, "version", mount.Version)
	writeOptionalMountDescriptorString(&b, "ref", mount.Ref)
	writeOptionalMountDescriptorString(&b, "revision", mount.Revision)
	writeOptionalMountDescriptorString(&b, "trust", mount.Trust)
	return b.String()
}

func writeMountDescriptorString(b *strings.Builder, key string, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeOptionalMountDescriptorString(b *strings.Builder, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	writeMountDescriptorString(b, key, value)
}

func LoadDescriptorMounts(root string) ([]Mount, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var files []string
	if err := filepath.WalkDir(rootAbs, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if filename != rootAbs && (strings.EqualFold(name, ".factile") || strings.EqualFold(name, ".git")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), mountDescriptorSuffix) {
			files = append(files, filename)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)

	type parsedDescriptor struct {
		filename string
		mount    Mount
		err      error
	}
	all := make([]parsedDescriptor, 0, len(files))
	for _, filename := range files {
		mount, err := LoadMountDescriptorFile(rootAbs, filename)
		all = append(all, parsedDescriptor{filename: filename, mount: mount, err: err})
	}

	validMounts := make([]Mount, 0, len(all))
	for _, descriptor := range all {
		if descriptor.err == nil {
			validMounts = append(validMounts, descriptor.mount)
		}
	}
	skipDirs := mountedSourceDirsWithinRoot(rootAbs, validMounts)
	seen := map[string]bool{}
	mounts := make([]Mount, 0, len(all))
	for _, descriptor := range all {
		if descriptorIsInMountedSource(descriptor.filename, skipDirs) {
			continue
		}
		if descriptor.err != nil {
			return nil, descriptor.err
		}
		mount := descriptor.mount
		if seen[mount.MountPath] {
			return nil, &Error{Code: "validation_failed", Message: "Duplicate mount descriptor path: " + mount.MountPath}
		}
		seen[mount.MountPath] = true
		mounts = append(mounts, mount)
	}
	sortMounts(mounts)
	return mounts, nil
}

func LoadMountDescriptorFile(root string, filename string) (Mount, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return Mount{}, err
	}
	filenameAbs, err := filepath.Abs(filename)
	if err != nil {
		return Mount{}, err
	}
	relFile, err := filepath.Rel(rootAbs, filenameAbs)
	if err != nil || relFile == "." || strings.HasPrefix(relFile, ".."+string(filepath.Separator)) || relFile == ".." || filepath.IsAbs(relFile) {
		return Mount{}, fmt.Errorf("mount descriptor %s is outside root %s", filenameAbs, rootAbs)
	}
	base := filepath.Base(filenameAbs)
	if !strings.HasSuffix(base, mountDescriptorSuffix) {
		return Mount{}, fmt.Errorf("mount descriptor filename must end with %s: %s", mountDescriptorSuffix, filenameAbs)
	}
	name := strings.TrimSuffix(base, mountDescriptorSuffix)
	if name == "" {
		return Mount{}, fmt.Errorf("mount descriptor filename has empty mount name: %s", filenameAbs)
	}
	relDir, err := filepath.Rel(rootAbs, filepath.Dir(filenameAbs))
	if err != nil {
		return Mount{}, err
	}
	mountPathInput := "/" + path.Join(filepath.ToSlash(relDir), name)
	if relDir == "." {
		mountPathInput = "/" + name
	}
	mountPath, err := ValidateMountPath(mountPathInput)
	if err != nil {
		return Mount{}, err
	}

	file, err := os.Open(filenameAbs)
	if err != nil {
		return Mount{}, err
	}
	defer file.Close()

	mount := Mount{MountPath: mountPath, RegistryPath: filenameAbs}
	writableSet := false
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return Mount{}, fmt.Errorf("unsupported mount descriptor table on line %d", lineNo)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return Mount{}, fmt.Errorf("invalid mount descriptor assignment on line %d", lineNo)
		}
		key := strings.TrimSpace(parts[0])
		rawValue := strings.TrimSpace(parts[1])
		if err := assignMountDescriptor(&mount, key, rawValue, lineNo, &writableSet); err != nil {
			return Mount{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return Mount{}, err
	}
	if mount.Source == "" {
		return Mount{}, fmt.Errorf("mount descriptor %s has no source", filepath.ToSlash(relFile))
	}
	if !writableSet {
		return Mount{}, fmt.Errorf("mount descriptor %s has no writable value", filepath.ToSlash(relFile))
	}
	classification, err := ClassifySource(mount.Source)
	if err != nil {
		return Mount{}, err
	}
	mount.Kind = classification.Kind
	if mount.Kind == "local" {
		source := mount.Source
		if !filepath.IsAbs(source) {
			source = filepath.Join(filepath.Dir(filenameAbs), source)
		}
		abs, err := filepath.Abs(source)
		if err != nil {
			return Mount{}, err
		}
		mount.SourcePath = filepath.Clean(abs)
	}
	return mount, nil
}

func assignMountDescriptor(mount *Mount, key string, rawValue string, lineNo int, writableSet *bool) error {
	switch key {
	case "source":
		value, err := parseMountString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		mount.Source = value
	case "writable":
		value, err := parseMountBool(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		mount.Writable = value
		*writableSet = true
	case "title":
		return assignMountDescriptorString(&mount.Title, rawValue, key, lineNo)
	case "description":
		return assignMountDescriptorString(&mount.Description, rawValue, key, lineNo)
	case "when_to_use":
		return assignMountDescriptorString(&mount.WhenToUse, rawValue, key, lineNo)
	case "when_not_to_use":
		return assignMountDescriptorString(&mount.WhenNotToUse, rawValue, key, lineNo)
	case "version":
		mount.VersionSet = true
		return assignMountDescriptorString(&mount.Version, rawValue, key, lineNo)
	case "ref":
		mount.RefSet = true
		return assignMountDescriptorString(&mount.Ref, rawValue, key, lineNo)
	case "revision":
		mount.RevisionSet = true
		return assignMountDescriptorString(&mount.Revision, rawValue, key, lineNo)
	case "trust":
		return assignMountDescriptorString(&mount.Trust, rawValue, key, lineNo)
	default:
		return fmt.Errorf("unsupported mount descriptor key %q on line %d", key, lineNo)
	}
	return nil
}

func assignMountDescriptorString(target *string, rawValue string, key string, lineNo int) error {
	value, err := parseMountString(rawValue, key, lineNo)
	if err != nil {
		return err
	}
	*target = value
	return nil
}

type mountedSourceDir struct {
	path  string
	owner string
}

func mountedSourceDirsWithinRoot(root string, mounts []Mount) []mountedSourceDir {
	var dirs []mountedSourceDir
	for _, mount := range mounts {
		if mount.Kind != "local" || mount.SourcePath == "" {
			continue
		}
		rel, err := filepath.Rel(root, mount.SourcePath)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			continue
		}
		dirs = append(dirs, mountedSourceDir{path: mount.SourcePath, owner: mount.RegistryPath})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].path < dirs[j].path
	})
	return dirs
}

func descriptorIsInMountedSource(filename string, sourceDirs []mountedSourceDir) bool {
	for _, dir := range sourceDirs {
		if filename == dir.owner {
			continue
		}
		rel, err := filepath.Rel(dir.path, filename)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}
