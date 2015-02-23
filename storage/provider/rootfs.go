// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provider

import (
	"os"
	"strconv"
	"strings"

	"github.com/juju/errors"

	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/storage"
	"io/ioutil"
)

const (
	RootfsProviderType = storage.ProviderType("rootfs")
)

// rootfsProviders create storage sources which provide access to filesystems.
type rootfsProvider struct {
	// run is a function type used for running commands on the local machine.
	run runCommandFunc
}

var (
	_ storage.Provider = (*rootfsProvider)(nil)
)

// ValidateConfig is defined on the Provider interface.
func (p *rootfsProvider) ValidateConfig(cfg *storage.Config) error {
	// Rootfs provider has no configuration.
	return nil
}

// validateFullConfig validates a fully-constructed storage config,
// combining the user-specified config and any internally specified
// config.
func (p *rootfsProvider) validateFullConfig(cfg *storage.Config) error {
	if err := p.ValidateConfig(cfg); err != nil {
		return err
	}
	storageDir, ok := cfg.ValueString(storage.ConfigStorageDir)
	if !ok || storageDir == "" {
		return errors.New("storage directory not specified")
	}
	return nil
}

// VolumeSource is defined on the Provider interface.
func (p *rootfsProvider) VolumeSource(environConfig *config.Config, providerConfig *storage.Config) (storage.VolumeSource, error) {
	return nil, errors.NotSupportedf("volumes")
}

// FilesystemSource is defined on the Provider interface.
func (p *rootfsProvider) FilesystemSource(environConfig *config.Config, sourceConfig *storage.Config) (storage.FilesystemSource, error) {
	if err := p.validateFullConfig(sourceConfig); err != nil {
		return nil, err
	}
	// storageDir is validated by validateFullConfig.
	storageDir, _ := sourceConfig.ValueString(storage.ConfigStorageDir)

	return &rootfsFilesystemSource{
		&osDirFuncs{},
		p.run,
		storageDir,
	}, nil
}

type rootfsFilesystemSource struct {
	dirFuncs   dirFuncs
	run        runCommandFunc
	storageDir string
}

// dirFuncs is used to allow the real directory operations to
// be stubbed out for testing.
type dirFuncs interface {
	mkDirAll(path string, perm os.FileMode) error
	lstat(path string) (fi os.FileInfo, err error)
	fileCount(path string) (int, error)
}

// The real directory related functions.
type osDirFuncs struct{}

func (*osDirFuncs) mkDirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (*osDirFuncs) lstat(path string) (fi os.FileInfo, err error) {
	return os.Lstat(path)
}

func (*osDirFuncs) fileCount(path string) (int, error) {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return 0, errors.Annotate(err, "could not read directory")
	}
	return len(files), nil
}

var _ storage.FilesystemSource = (*rootfsFilesystemSource)(nil)

// ValidateFilesystemParams is defined on the FilesystemSource interface.
func (s *rootfsFilesystemSource) ValidateFilesystemParams(params storage.FilesystemParams) error {
	// ValidateFilesystemParams may be called on a machine other than the
	// machine where the filesystem will be mounted, so we cannot check
	// available size until we get to CreateFilesystem.
	if params.Attachment == nil {
		return errors.NotSupportedf(
			"creating filesystem without machine attachment",
		)
	}
	return nil
}

// CreateFilesystems is defined on the FilesystemSource interface.
func (s *rootfsFilesystemSource) CreateFilesystems(args []storage.FilesystemParams,
) ([]storage.Filesystem, []storage.FilesystemAttachment, error) {
	filesystems := make([]storage.Filesystem, 0, len(args))
	filesystemAttachments := make([]storage.FilesystemAttachment, 0, len(args))
	for _, arg := range args {
		filesystem, filesystemAttachment, err := s.createFilesystem(arg)
		if err != nil {
			return nil, nil, errors.Annotate(err, "creating filesystem")
		}
		filesystems = append(filesystems, filesystem)
		filesystemAttachments = append(filesystemAttachments, filesystemAttachment)
	}
	return filesystems, filesystemAttachments, nil

}

func (s *rootfsFilesystemSource) createFilesystem(params storage.FilesystemParams) (storage.Filesystem, storage.FilesystemAttachment, error) {
	var filesystem storage.Filesystem
	var filesystemAttachment storage.FilesystemAttachment
	if err := s.ValidateFilesystemParams(params); err != nil {
		return filesystem, filesystemAttachment, errors.Trace(err)
	}
	path := params.Attachment.Path
	if path == "" {
		return filesystem, filesystemAttachment, errors.New("cannot create a filesystem mount without specifying a path")
	}
	if err := s.validatePath(path); err != nil {
		return filesystem, filesystemAttachment, err
	}
	filesystemAttachment = storage.FilesystemAttachment{
		Filesystem: params.Tag,
		Machine:    params.Attachment.Machine,
		Path:       path,
	}
	dfOutput, err := s.run("df", "--output=size", path)
	if err != nil {
		os.Remove(path)
		return filesystem, filesystemAttachment, errors.Annotate(err, "getting size")
	}
	lines := strings.SplitN(dfOutput, "\n", 2)
	blocks, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		os.Remove(path)
		return filesystem, filesystemAttachment, errors.Annotate(err, "parsing size")
	}
	sizeInMiB := blocks / 1024
	if sizeInMiB < params.Size {
		os.Remove(path)
		return filesystem, filesystemAttachment, errors.Errorf("filesystem is not big enough (%dM < %dM)", sizeInMiB, params.Size)
	}

	filesystem = storage.Filesystem{
		Tag:  params.Tag,
		Size: sizeInMiB,
	}
	return filesystem, filesystemAttachment, nil
}

func (s *rootfsFilesystemSource) validatePath(path string) error {
	// If path already exists, we check that it is empty.
	// It is up to the storage provisioner to ensure that any
	// shared storage constraints and attachments with the same
	// path are validated etc. So the check here is more a sanity check.
	if fi, err := s.dirFuncs.lstat(path); os.IsNotExist(err) {
		if err := s.dirFuncs.mkDirAll(path, 0755); err != nil {
			return errors.Annotate(err, "could not create directory")
		}
	} else if !fi.IsDir() {
		return errors.Errorf("path %q must be a directory", path)
	} else {
		fileCount, err := s.dirFuncs.fileCount(path)
		if err != nil {
			return errors.Annotate(err, "could not read directory")
		}
		if fileCount > 0 {
			return errors.New("path must be empty")
		}
	}
	return nil
}