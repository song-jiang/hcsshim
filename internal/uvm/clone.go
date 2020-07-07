package uvm

import (
	"context"
	"fmt"

	"github.com/Microsoft/hcsshim/internal/cow"
	hcsschema "github.com/Microsoft/hcsshim/internal/schema2"
	"github.com/pkg/errors"
)

const (
	hcsSaveOptions = "{\"SaveType\": \"AsTemplate\"}"
)

// Cloneable is a generic interface for cloning a specific resource. Not all resources can
// be cloned and so all resources might not implement this interface. This interface is
// mainly used during late cloning process to clone the resources associated with the UVM
// and the container. For some resources (like scratch VHDs of the UVM & container)
// cloning means actually creating a copy of that resource while for some resources it
// simply means adding that resource to the cloned VM without copying (like VSMB shares).
// The Clone function of that resource will deal with these details.
type Cloneable interface {
	// Clone function creates a clone of the resource on the UVM `vm` and returns a
	// pointer to the struct that represents the cloned resource.
	// `cd` parameter can be used to pass any other data that is required during the
	// cloning process of that resource (for example, when cloning SCSI Mounts we
	// might need scratchFolder).
	// Clone function should be called on a valid struct (Mostly on the struct which
	// is deserialized, and so Clone function should only depend on the fields that are
	// exported in the struct).
	// The implementation of the clone function should avoid reading any data from the
	// `vm` struct, it can add new fields to the vm struct but since the vm struct
	// isn't fully ready at this point it shouldn't be used to read any data.
	Clone(ctx context.Context, vm *UtilityVM, cd *CloneData) (interface{}, error)
}

// A struct to keep all the information that might be required during cloning process of
// a resource.
type CloneData struct {
	// doc spec for the clone
	doc *hcsschema.ComputeSystem
	// scratchFolder of the clone
	scratchFolder string
	// UVMID of the clone
	uvmID string
}

// UVMTemplateConfig is just a wrapper struct that keeps together all the resources that
// need to be saved to create a template.
type UVMTemplateConfig struct {
	// ID of the template vm
	UVMID string
	// Array of all resources that will be required while making a clone from this template
	Resources []Cloneable
}

// Captures all the information that is necessary to properly save this UVM as a template
// and create clones from this template later. The struct returned by this method must be
// later on made available while creating a clone from this template.
func (uvm *UtilityVM) GenerateTemplateConfig() *UVMTemplateConfig {
	// Add all the SCSI Mounts and VSMB shares into the list of clones
	templateConfig := &UVMTemplateConfig{
		UVMID: uvm.ID(),
	}

	for _, vsmbShare := range uvm.vsmbDirShares {
		if vsmbShare != nil {
			templateConfig.Resources = append(templateConfig.Resources, vsmbShare)
		}
	}

	for _, vsmbShare := range uvm.vsmbFileShares {
		if vsmbShare != nil {
			templateConfig.Resources = append(templateConfig.Resources, vsmbShare)
		}
	}

	for _, location := range uvm.scsiLocations {
		for _, scsiMount := range location {
			if scsiMount != nil {
				templateConfig.Resources = append(templateConfig.Resources, scsiMount)
			}
		}
	}

	return templateConfig
}

// Pauses the uvm and then saves it as a template. This uvm can not be restarted or used
// after it is successfully saved.
func (uvm *UtilityVM) SaveAsTemplate(ctx context.Context) error {
	err := uvm.hcsSystem.Pause(ctx)
	if err != nil {
		return errors.Wrap(err, "error pausing the VM")
	}

	err = uvm.hcsSystem.Save(ctx, hcsSaveOptions)
	if err != nil {
		return errors.Wrap(err, "error saving the VM")
	}
	return nil
}

// CloneContainer attaches back to a container that is already running inside the UVM
// because of the clone
func (uvm *UtilityVM) CloneContainer(ctx context.Context, id string) (cow.Container, error) {
	if uvm.gc == nil {
		return nil, fmt.Errorf("clone container cannot work without external GCS connection")
	}
	c, err := uvm.gc.CloneContainer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to clone container %s: %s", id, err)
	}
	return c, nil
}
