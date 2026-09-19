//go:build unix

package agentfs

import (
	"errors"

	"github.com/pbs-plus/pxar/format"
	"golang.org/x/sys/unix"
)

func nativePlatformXAttrs(path string) ([]format.XAttr, error) {
	size, err := unix.Llistxattr(path, nil)
	if err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENODATA) {
			return nil, nil
		}
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}

	names := make([]byte, size)
	n, err := unix.Llistxattr(path, names)
	if err != nil {
		return nil, err
	}
	names = names[:n]

	attrs := make([]format.XAttr, 0, 8)
	for len(names) > 0 {
		end := 0
		for end < len(names) && names[end] != 0 {
			end++
		}
		if end == 0 {
			names = names[1:]
			continue
		}
		name := string(names[:end])
		if end < len(names) {
			names = names[end+1:]
		} else {
			names = nil
		}

		// POSIX ACLs and capabilities have dedicated PXAR fields.
		if name == XATTR_NAME_ACL_ACCESS || name == XATTR_NAME_ACL_DEFAULT || name == "security.capability" {
			continue
		}
		valueSize, err := unix.Lgetxattr(path, name, nil)
		if err != nil {
			return nil, err
		}
		if valueSize < 0 {
			return nil, unix.EIO
		}
		value := make([]byte, valueSize)
		if valueSize != 0 {
			got, err := unix.Lgetxattr(path, name, value)
			if err != nil {
				return nil, err
			}
			value = value[:got]
		}
		attrs = append(attrs, format.NewXAttr([]byte(name), value))
	}
	return attrs, nil
}

func nativePlatformFCaps(path string) ([]byte, error) {
	size, err := unix.Lgetxattr(path, "security.capability", nil)
	if err != nil {
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
			return nil, nil
		}
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	data := make([]byte, size)
	n, err := unix.Lgetxattr(path, "security.capability", data)
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}
