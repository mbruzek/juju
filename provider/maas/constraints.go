// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package maas

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/utils/set"

	"github.com/juju/juju/constraints"
)

var unsupportedConstraints = []string{
	constraints.CpuPower,
	constraints.InstanceType,
}

// ConstraintsValidator is defined on the Environs interface.
func (environ *maasEnviron) ConstraintsValidator() (constraints.Validator, error) {
	validator := constraints.NewValidator()
	validator.RegisterUnsupported(unsupportedConstraints)
	supportedArches, err := environ.SupportedArchitectures()
	if err != nil {
		return nil, err
	}
	validator.RegisterVocabulary(constraints.Arch, supportedArches)
	return validator, nil
}

// convertConstraints converts the given constraints into an url.Values object
// suitable to pass to MAAS when acquiring a node. CpuPower is ignored because
// it cannot be translated into something meaningful for MAAS right now.
func convertConstraints(cons constraints.Value) url.Values {
	params := url.Values{}
	if cons.Arch != nil {
		// Note: Juju and MAAS use the same architecture names.
		// MAAS also accepts a subarchitecture (e.g. "highbank"
		// for ARM), which defaults to "generic" if unspecified.
		params.Add("arch", *cons.Arch)
	}
	if cons.CpuCores != nil {
		params.Add("cpu_count", fmt.Sprintf("%d", *cons.CpuCores))
	}
	if cons.Mem != nil {
		params.Add("mem", fmt.Sprintf("%d", *cons.Mem))
	}
	convertTagsToParams(params, cons.Tags)
	if cons.CpuPower != nil {
		logger.Warningf("ignoring unsupported constraint 'cpu-power'")
	}
	return params
}

// convertTagsToParams converts a list of positive/negative tags from
// constraints into two comma-delimited lists of values, which can then be
// passed to MAAS using the "tags" and "not_tags" arguments to acquire. If
// either list of tags is empty, the respective argument is not added to params.
func convertTagsToParams(params url.Values, tags *[]string) {
	if tags == nil || len(*tags) == 0 {
		return
	}
	positives, negatives := parseDelimitedValues(*tags)
	if len(positives) > 0 {
		params.Add("tags", strings.Join(positives, ","))
	}
	if len(negatives) > 0 {
		params.Add("not_tags", strings.Join(negatives, ","))
	}
}

// convertSpacesToBindings converts a list of positive/negative spaces from
// constraints into a list of interface bindings with zero-based numeric labels
// and using "space=<positive-name>" or "not_space=<negative-name>". Those
// bindings can then be passed to MAAS acquire node API.
func convertSpacesToBindings(spaces *[]string) []interfaceBinding {
	if spaces == nil || len(*spaces) == 0 {
		return nil
	}
	var index uint
	var bindings []interfaceBinding
	positives, negatives := parseDelimitedValues(*spaces)
	for _, space := range positives {
		bindings = append(bindings, interfaceBinding{
			Name:            fmt.Sprintf("%v", index),
			SpaceProviderId: space,
			IsExcluded:      false,
		})
		index++
	}
	for _, space := range negatives {
		bindings = append(bindings, interfaceBinding{
			Name:            fmt.Sprintf("%v", index),
			SpaceProviderId: space,
			IsExcluded:      true,
		})
		index++
	}
	return bindings
}

// parseDelimitedValues parses a slice of raw values coming from constraints
// (Tags or Spaces). The result is split into two slices - positives and
// negatives (prefixed with "^"). Empty values are ignored.
func parseDelimitedValues(rawValues []string) (positives, negatives []string) {
	for _, value := range rawValues {
		if value == "" || value == "^" {
			// Neither of these cases should happen in practise, as constraints
			// are validated before setting them and empty names for spaces or
			// tags are not allowed.
			continue
		}
		if strings.HasPrefix(value, "^") {
			negatives = append(negatives, strings.TrimPrefix(value, "^"))
		} else {
			positives = append(positives, value)
		}
	}
	return positives, negatives
}

// interfaceBinding defines a requirement that a node interface must satisfy in
// order for that node to get selected and started, based on deploy-time
// bindings of a service.
//
// TODO(dimitern): Once the services have bindings defined in state, a version
// of this should go to the network package (needs to be non-MAAS-specifc
// first). Also, we need to transform Juju space names from constraints into
// MAAS space provider IDs.
type interfaceBinding struct {
	Name            string
	SpaceProviderId string

	// IsExcluded is true if the space has to be excluded from this binding.
	IsExcluded bool

	// add more as needed.
}

// addInterfaces converts a slice of interface bindings to the format MAAS
// expects for the "interfaces" and "not_networks" arguments to acquire node.
// Returns an error satisfying errors.IsNotValid() if bindings contains
// duplicates or empty Name/SpaceName.
func addInterfaces(params url.Values, bindings []interfaceBinding) error {
	if len(bindings) == 0 {
		return nil
	}
	var positives []string
	var negatives []string
	namesSet := set.NewStrings()
	for _, binding := range bindings {
		switch {
		case binding.Name == "":
			return errors.NewNotValid(nil, "interface bindings cannot have empty names")
		case binding.SpaceProviderId == "":
			return errors.NewNotValid(nil, fmt.Sprintf(
				"invalid interface binding %q: space provider ID is required", binding.Name),
			)
		case namesSet.Contains(binding.Name):
			return errors.NewNotValid(nil, fmt.Sprintf(
				"duplicated interface binding %q", binding.Name),
			)
		}
		namesSet.Add(binding.Name)
		if binding.IsExcluded {
			negatives = append(negatives, fmt.Sprintf("space:%s", binding.SpaceProviderId))
		} else {
			positives = append(positives, fmt.Sprintf("%s:space=%s", binding.Name, binding.SpaceProviderId))
		}
	}
	if len(positives) > 0 {
		params.Add("interfaces", strings.Join(positives, ";"))
	}
	if len(negatives) > 0 {
		params.Add("not_networks", strings.Join(negatives, ","))
	}
	return nil
}

// addStorage converts volume information into url.Values object suitable to
// pass to MAAS when acquiring a node.
func addStorage(params url.Values, volumes []volumeInfo) {
	if len(volumes) == 0 {
		return
	}
	// Requests for specific values are passed to the acquire URL
	// as a storage URL parameter of the form:
	// [volume-name:]sizeinGB[tag,...]
	// See http://maas.ubuntu.com/docs/api.html#nodes

	// eg storage=root:0(ssd),data:20(magnetic,5400rpm),45
	makeVolumeParams := func(v volumeInfo) string {
		var params string
		if v.name != "" {
			params = v.name + ":"
		}
		params += fmt.Sprintf("%d", v.sizeInGB)
		if len(v.tags) > 0 {
			params += fmt.Sprintf("(%s)", strings.Join(v.tags, ","))
		}
		return params
	}
	var volParms []string
	for _, v := range volumes {
		params := makeVolumeParams(v)
		volParms = append(volParms, params)
	}
	params.Add("storage", strings.Join(volParms, ","))
}
