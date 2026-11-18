package agentfs

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Do not embed *os.File: its WriteTo optimization would bypass Read validation.
type checkedNativeFile struct {
	file   *os.File
	before os.FileInfo
	change [2]int64
	read   int64
	done   bool
	closed bool
	err    error
}

func newCheckedNativeFile(file *os.File) (*checkedNativeFile, error) {
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() < 0 {
		return nil, fmt.Errorf("not a sized regular file: %s", file.Name())
	}
	change, err := nativeReadChange(file)
	if err != nil {
		return nil, err
	}
	return &checkedNativeFile{file: file, before: before, change: change}, nil
}

func (r *checkedNativeFile) validate() error {
	after, err := r.file.Stat()
	if err != nil {
		return err
	}
	change, err := nativeReadChange(r.file)
	if err != nil {
		return err
	}
	if r.before.Size() != after.Size() || !r.before.ModTime().Equal(after.ModTime()) ||
		r.before.Mode() != after.Mode() || r.change != change {
		return fmt.Errorf("source file changed while being backed up: %s", r.file.Name())
	}
	return nil
}

func (r *checkedNativeFile) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.done {
		return 0, io.EOF
	}
	n, err := r.file.Read(p)
	r.read += int64(n)
	if r.read > r.before.Size() || (err == io.EOF && r.read != r.before.Size()) {
		r.err = fmt.Errorf("source file size changed while being backed up: %s", r.file.Name())
		return 0, r.err
	}
	if err == io.EOF || r.read == r.before.Size() {
		if r.err = r.validate(); r.err != nil {
			return 0, r.err
		}
		r.done = true
	}
	return n, err
}

func (r *checkedNativeFile) Close() error {
	if r.closed {
		return r.err
	}
	r.closed = true
	var validation error
	if r.read != r.before.Size() {
		validation = fmt.Errorf("source file was not fully read: %s", r.file.Name())
	} else {
		// Catch changes after the final Read, before the encoder commits.
		validation = r.validate()
	}
	r.err = errors.Join(r.err, validation, r.file.Close())
	return r.err
}
