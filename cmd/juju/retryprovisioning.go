// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"fmt"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/cmd/envcmd"
)

// RetryProvisioningCommand updates machines' error status to tell
// the provisoner that it should try to re-provision the machine.
type RetryProvisioningCommand struct {
	envcmd.EnvCommandBase
	Machines []string
}

func (c *RetryProvisioningCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "retry-provisioning",
		Args:    "<machine> [...]",
		Purpose: "retries provisioning for failed machines",
	}
}

func (c *RetryProvisioningCommand) Init(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no machine specified")
	}
	c.Machines = make([]string, len(args))
	for i, arg := range args {
		if !names.IsValidMachine(arg) {
			return fmt.Errorf("invalid machine %q", arg)
		}
		c.Machines[i] = names.NewMachineTag(arg).String()
	}
	return nil
}

func (c *RetryProvisioningCommand) Run(context *cmd.Context) error {
	client, err := c.NewAPIClient()
	if err != nil {
		return err
	}
	defer client.Close()

	var machines []names.MachineTag
	for _, tag := range c.Machines {
		machine, err := names.ParseMachineTag(tag)
		if err != nil {
			return errors.Trace(err)
		}
		machines = append(machines, machine)
	}

	results, err := client.RetryProvisioning(machines...)
	if err != nil {
		return err
	}
	for i, result := range results {
		if result.Error != nil {
			fmt.Fprintf(context.Stderr, "cannot retry provisioning %q: %v\n", c.Machines[i], result.Error)
		}
	}
	return nil
}
