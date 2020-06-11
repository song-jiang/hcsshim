package hcsoci

import (
	"context"

	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/sirupsen/logrus"
)

func createNetworkNamespace(ctx context.Context, coi *createOptionsInternal, resources *Resources) error {
	op := "hcsoci::createNetworkNamespace"
	l := log.G(ctx).WithField(logfields.ContainerID, coi.ID)
	l.Debug(op + " - Begin")
	defer func() {
		l.Debug(op + " - End")
	}()

	netID, err := hns.CreateNamespace()
	if err != nil {
		return err
	}
	log.G(ctx).WithFields(logrus.Fields{
		"netID":               netID,
		logfields.ContainerID: coi.ID,
	}).Info("created network namespace for container")
	resources.netNS = netID
	resources.createdNetNS = true
	endpoints := make([]string, 0)
	for _, endpointID := range coi.Spec.Windows.Network.EndpointList {
		err = hns.AddNamespaceEndpoint(netID, endpointID)
		if err != nil {
			return err
		}
		log.G(ctx).WithFields(logrus.Fields{
			"netID":      netID,
			"endpointID": endpointID,
		}).Info("added network endpoint to namespace")
		endpoints = append(endpoints, endpointID)
	}
	resources.resources = append(resources.resources, &uvm.NetworkEndpoints{EndpointIDs: endpoints, Namespace: netID})
	return nil
}

// GetNamespaceEndpoints gets all endpoints in `netNS`
func GetNamespaceEndpoints(ctx context.Context, netNS string) ([]*hns.HNSEndpoint, error) {
	op := "hcsoci::GetNamespaceEndpoints"
	l := log.G(ctx).WithField("netns-id", netNS)
	l.Debug(op + " - Begin")
	defer func() {
		l.Debug(op + " - End")
	}()

	ids, err := hns.GetNamespaceEndpoints(netNS)
	if err != nil {
		return nil, err
	}
	var endpoints []*hns.HNSEndpoint
	for _, id := range ids {
		endpoint, err := hns.GetHNSEndpointByID(id)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// Network namespace setup is a bit different for templates and clones.
// A normal network namespace creation works as follows: When a new Pod is created, HNS is
// called to create a new namespace and endpoints inside that namespace. Then we hot add
// this network namespace and then the network endpoints associated with that namespace
// into this pod UVM. Later when we create containers inside that pod they start running
// inside this namespace. The information about namespace and endpoints is maintained by
// the HNS and it is stored in the guest VM's registry (UVM) as well. So if inside the pod
// we see a namespcae with id 'NSID' then we can query HNS to find all the information
// about that namespace.
//
// When we clone a VM (with containers running inside it) we can hot add a new namespace
// and endpoints created for that namespace to the uvm but we can't make the existing
// processes/containers to automatically switch over to this new namespace. This means
// that all clones will have the same namespace ID as that of the template from which they
// are created. This can make debugging very difficult when we have multiple UVMs with
// same NSID.  To make debugging a bit easier we will create all templates and clones with
// a special NSID that is dedicated for cloning. So during debugging if we see multiple
// UVMs with same NSID we can quickly check if this is because of cloning. Now inside the
// HNS you can not have multiple UVMs using the same NSID. So to achieve this when we
// create a template or a cloned pod we will ask HNS to create a new namespace and
// endpoints for that UVM but when we actually send a request to hot add that namespace we
// will change the namespace ID with the ID that is specifically created for cloning
// purposes. Similarly, when hot adding an endpoint we will modify this endpoint
// information to set its network namespace ID to this default ID. This way inside every
// template and cloned pod the namespace ID will remain same (but each cloned UVM will
// have a different endpoints) but the HNS will have the actual namespace ID that was
// created for that UVM.
//
// In this function we take the namespace ID of the namespace that was created for this
// UVM. We hot add the namespace (with the default ID if this is a template). We get the
// endpoints associated with this namespace and then hot add those endpoints (by changing
// their namespace IDs by the deafult IDs if it is a template).
func SetupNetworkNamespace(ctx context.Context, hostingSystem *uvm.UtilityVM, nsid string, isTemplate, isClone bool) error {
	nsidInsideUVM := nsid
	if isTemplate || isClone {
		nsidInsideUVM = hns.CLONING_DEFAULT_NETWORK_NAMESPACE_ID
	}

	// Query endpoints with actual nsid
	endpoints, err := GetNamespaceEndpoints(ctx, nsid)
	if err != nil {
		return err
	}

	// Add the network namespace inside the UVM if it is not a clone. (Clones willl
	// inherit the namespace from template)
	if !isClone {
		// Get the namespace struct from the actual nsid.
		hcnNamespace, err := hcn.GetNamespaceByID(nsid)
		if err != nil {
			return err
		}

		// All templates should have a special NSID so that it
		// will be easier to debug. Override it here.
		if isTemplate {
			hcnNamespace.Id = nsidInsideUVM
		}

		if err = hostingSystem.AddNetNSRaw(ctx, hcnNamespace); err != nil {
			return err
		}
	}

	// If adding a network endpoint to clones or a template override nsid associated
	// with it.
	if isClone || isTemplate {
		// replace nsid for each endpoint
		for _, ep := range endpoints {
			ep.Namespace = &hns.Namespace{
				ID: nsidInsideUVM,
			}
		}
	}

	if err = hostingSystem.AddEndpointsToNS(ctx, nsidInsideUVM, endpoints); err != nil {
		// Best effort clean up the NS
		if removeErr := hostingSystem.RemoveNetNS(ctx, nsidInsideUVM); removeErr != nil {
			log.G(ctx).Warn(removeErr)
		}
		return err
	}
	return nil
}
