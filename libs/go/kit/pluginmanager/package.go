package pluginmanager

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const packageDescriptor = "plugin.json"

// CreatePackage creates a portable Orion plugin package. The archive contains
// plugin.json and bin/<plugin-id>; it has no container/runtime dependency.
func CreatePackage(spec Spec, output string) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if output == "" {
		return errors.New("plugin package output is required")
	}
	info, err := os.Stat(spec.Binary)
	if err != nil {
		return fmt.Errorf("stat plugin binary: %w", err)
	}
	if info.IsDir() {
		return errors.New("plugin binary must be a file")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create package directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".orion-plugin-*.tmp")
	if err != nil {
		return fmt.Errorf("create package: %w", err)
	}
	tmpName := file.Name()
	defer os.Remove(tmpName)
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)

	descriptor := spec
	descriptor.Binary = filepath.ToSlash(filepath.Join("bin", spec.ID))
	descriptor.Manifest = ""
	descriptorData, err := json.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		file.Close()
		return fmt.Errorf("encode plugin descriptor: %w", err)
	}
	if err := writeTarBytes(tw, packageDescriptor, descriptorData, 0o644); err != nil {
		file.Close()
		return err
	}

	binary, err := os.Open(spec.Binary)
	if err != nil {
		file.Close()
		return fmt.Errorf("open plugin binary: %w", err)
	}
	hash := sha256.New()
	header := &tar.Header{Name: descriptor.Binary, Mode: 0o755, Size: info.Size()}
	if err := tw.WriteHeader(header); err != nil {
		binary.Close()
		file.Close()
		return fmt.Errorf("write plugin binary header: %w", err)
	}
	if _, err := io.Copy(tw, io.TeeReader(binary, hash)); err != nil {
		binary.Close()
		file.Close()
		return fmt.Errorf("write plugin binary: %w", err)
	}
	if err := binary.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close plugin binary: %w", err)
	}
	checksum := []byte(hex.EncodeToString(hash.Sum(nil)) + "  " + descriptor.Binary + "\n")
	if err := writeTarBytes(tw, "checksums.sha256", checksum, 0o644); err != nil {
		file.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close plugin archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		file.Close()
		return fmt.Errorf("close plugin compression: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close plugin package: %w", err)
	}
	if err := os.Rename(tmpName, output); err != nil {
		return fmt.Errorf("publish plugin package: %w", err)
	}
	return nil
}

// InstallPackage extracts a package into the manager-owned directory and
// installs the resulting executable. Archive paths are validated before any
// file is created, preventing traversal outside the package directory.
func (m *Manager) InstallPackage(packagePath string) (Spec, error) {
	archive, err := os.Open(packagePath)
	if err != nil {
		return Spec{}, fmt.Errorf("open plugin package: %w", err)
	}
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return Spec{}, fmt.Errorf("read plugin package compression: %w", err)
	}
	tr := tar.NewReader(gz)
	var descriptor Spec
	files := make(map[string][]byte)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Spec{}, fmt.Errorf("read plugin package entry: %w", err)
		}
		if err := validateArchivePath(header.Name); err != nil {
			return Spec{}, err
		}
		if header.Typeflag != tar.TypeReg {
			return Spec{}, fmt.Errorf("plugin package entry %q is not a regular file", header.Name)
		}
		data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
		if err != nil {
			return Spec{}, fmt.Errorf("read plugin package entry %q: %w", header.Name, err)
		}
		files[header.Name] = data
	}
	if err := json.Unmarshal(files[packageDescriptor], &descriptor); err != nil {
		return Spec{}, fmt.Errorf("decode plugin descriptor: %w", err)
	}
	if err := validateSpec(descriptor); err != nil {
		return Spec{}, err
	}
	if !strings.HasPrefix(filepath.ToSlash(descriptor.Binary), "bin/") {
		return Spec{}, errors.New("plugin package binary must be under bin/")
	}
	binaryData, ok := files[filepath.ToSlash(descriptor.Binary)]
	if !ok {
		return Spec{}, fmt.Errorf("plugin package is missing %q", descriptor.Binary)
	}
	if expected := strings.TrimSpace(string(files["checksums.sha256"])); expected != "" {
		parts := strings.Fields(expected)
		if len(parts) < 1 || parts[0] != fmt.Sprintf("%x", sha256.Sum256(binaryData)) {
			return Spec{}, errors.New("plugin package checksum verification failed")
		}
	}

	packageRoot := filepath.Join(m.statePath+".d", "packages", safePackageName(descriptor.ID+"-"+descriptor.Version))
	if err := os.MkdirAll(filepath.Dir(packageRoot), 0o755); err != nil {
		return Spec{}, fmt.Errorf("create plugin package directory: %w", err)
	}
	tempRoot, err := os.MkdirTemp(filepath.Dir(packageRoot), ".extract-*")
	if err != nil {
		return Spec{}, fmt.Errorf("create plugin extraction directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(tempRoot)
		}
	}()
	for name, data := range files {
		if name == "checksums.sha256" || name == packageDescriptor {
			continue
		}
		destination := filepath.Join(tempRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return Spec{}, fmt.Errorf("create extracted plugin directory: %w", err)
		}
		if err := os.WriteFile(destination, data, 0o755); err != nil {
			return Spec{}, fmt.Errorf("write extracted plugin file: %w", err)
		}
	}
	if _, err := os.Stat(packageRoot); err == nil {
		return Spec{}, fmt.Errorf("plugin package %s is already installed", descriptor.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Spec{}, fmt.Errorf("check installed plugin package: %w", err)
	}
	if err := os.Rename(tempRoot, packageRoot); err != nil {
		return Spec{}, fmt.Errorf("publish extracted plugin: %w", err)
	}
	keep = true
	descriptor.Binary = filepath.Join(packageRoot, filepath.FromSlash(descriptor.Binary))
	descriptor.Manifest = ""
	if err := m.Install(descriptor); err != nil {
		return Spec{}, err
	}
	return descriptor, nil
}

func writeTarBytes(tw *tar.Writer, name string, data []byte, mode int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		return fmt.Errorf("write package entry %q: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write package entry %q: %w", name, err)
	}
	return nil
}

func validateArchivePath(name string) error {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || filepath.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe plugin package path %q", name)
	}
	return nil
}

func safePackageName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
