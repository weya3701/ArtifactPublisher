package packagehandler

import (
	"context"

	"packagespublisher/internal/model"
)

type Handler interface {
	Detect(string) bool
	ParseMetadata(context.Context, string) (model.PackageDescriptor, error)
	ValidateCompleteness(model.PackageDescriptor) error
	BuildPackageDescriptor(context.Context, string) (model.PackageDescriptor, error)
}
