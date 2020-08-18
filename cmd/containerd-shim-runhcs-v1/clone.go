package main

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/clone"
	"github.com/Microsoft/hcsshim/internal/uvm"
)

// saveAsTemplate saves the UVM and container inside it as a template and also stores
// the relevant information in the registry so that clones can be created from this
// template.
// Saving is done in following steps:
// - First remove all the NICs associated with the host.
// - Close the GCS connection.
// - Save the information about the templtae that will be needed during cloning
// - Save the host as a template.
func saveAsTemplate(ctx context.Context, host *uvm.UtilityVM) (err error) {
	if err = host.RemoveAllNICs(ctx); err != nil {
		return err
	}

	if err = host.CloseGCSConnection(); err != nil {
		return err
	}

	if err = clone.SaveTemplateConfig(ctx, host.GenerateTemplateConfig()); err != nil {
		return err
	}

	if err = host.SaveAsTemplate(ctx); err != nil {
		return err
	}
	return nil
}
