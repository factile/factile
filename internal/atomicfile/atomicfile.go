package atomicfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type operations struct {
	rename func(string, string) error
	link   func(string, string) error
}

var systemOperations = operations{rename: os.Rename, link: os.Link}

// Write publishes a complete replacement using a temporary file in the same
// directory. The destination remains at its old contents if publication fails.
func Write(filename string, data []byte, mode os.FileMode) error {
	return writeWithOperations(filename, data, mode, systemOperations)
}

// Create publishes a complete file only when the destination is still absent.
// It returns false without changing the destination when another file won the
// create race.
func Create(filename string, data []byte, mode os.FileMode) (bool, error) {
	return createWithOperations(filename, data, mode, systemOperations)
}

func writeWithOperations(filename string, data []byte, mode os.FileMode, ops operations) error {
	temporary, err := stage(filename, data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	return ops.rename(temporary, filename)
}

func createWithOperations(filename string, data []byte, mode os.FileMode, ops operations) (bool, error) {
	temporary, err := stage(filename, data, mode)
	if err != nil {
		return false, err
	}
	defer os.Remove(temporary)
	if err := ops.link(temporary, filename); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func stage(filename string, data []byte, mode os.FileMode) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+".factile-tmp-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	ok := false
	defer func() {
		if !ok {
			temporary.Close()
			os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return "", err
	}
	written, err := temporary.Write(data)
	if err != nil {
		return "", err
	}
	if written != len(data) {
		return "", io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	ok = true
	return temporaryName, nil
}
