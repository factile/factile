package factile

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

const (
	ErrInvalidPath             = "invalid_path"
	ErrMountNotFound           = "mount_not_found"
	ErrNoActiveRoot            = "no_active_root"
	ErrAmbiguousTarget         = "ambiguous_target"
	ErrConceptNotFound         = "concept_not_found"
	ErrConceptAlreadyExist     = "concept_already_exists"
	ErrPathAlreadyExists       = "path_already_exists"
	ErrPathIsNotConcept        = "path_is_not_concept"
	ErrPathIsNotBundle         = "path_is_not_bundle"
	ErrRevisionRequired        = "revision_required"
	ErrRevisionMismatch        = "revision_mismatch"
	ErrSourceReadOnly          = "source_read_only"
	ErrValidationFailed        = "validation_failed"
	ErrLockTimeout             = "lock_timeout"
	ErrPartialFailure          = "partial_failure"
	ErrUnsafeSourcePath        = "unsafe_source_path"
	ErrOKFParse                = "okf_parse_error"
	ErrSectionNotFound         = "section_not_found"
	ErrUnsupportedSource       = "unsupported_source"
	ErrUnsupportedCommand      = "unsupported_command"
	ErrRemoteSourceUnavailable = "remote_source_unavailable"
	ErrRevisionNotAvailable    = "revision_not_available"
)

type AppError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return e.Message
}

func NewError(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func ErrorCode(err error) string {
	var app *AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return "general_failure"
}

func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	var app *AppError
	if errors.As(err, &app) {
		return app
	}
	var vfsErr *vfs.Error
	if errors.As(err, &vfsErr) {
		return &AppError{Code: vfsErr.Code, Message: vfsErr.Message}
	}
	if errors.Is(err, os.ErrNotExist) {
		return NewError(ErrConceptNotFound, "Concept not found")
	}
	if errors.Is(err, os.ErrExist) {
		return NewError(ErrConceptAlreadyExist, "Concept already exists")
	}
	if errors.Is(err, storage.ErrUnsafePath) {
		return NewError(ErrUnsafeSourcePath, err.Error())
	}
	if errors.Is(err, storage.ErrLockTimeout) {
		return NewError(ErrLockTimeout, err.Error())
	}
	if errors.Is(err, okf.ErrMissingFrontmatter) || errors.Is(err, okf.ErrInvalidFrontmatter) {
		return NewError(ErrOKFParse, err.Error())
	}
	if strings.Contains(err.Error(), "lock timeout") {
		return NewError(ErrLockTimeout, err.Error())
	}
	return err
}

func errorf(code, format string, args ...any) *AppError {
	return NewError(code, fmt.Sprintf(format, args...))
}
