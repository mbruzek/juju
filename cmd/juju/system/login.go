// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package system

import (
	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/juju/api/usermanager"
	"github.com/juju/names"
	"github.com/juju/utils"
	goyaml "gopkg.in/yaml.v1"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/api"
	"github.com/juju/juju/cmd/envcmd"
	"github.com/juju/juju/environs/configstore"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/network"
)

// ServerFile format
// This will need to move when the user manager commands generate
// this file format.  The file format is expected to be YAML.
type ServerFile struct {
	Addresses []string `yaml:"addresses"`
	CACert    string   `yaml:"ca-cert,omitempty"`
	Username  string   `yaml:"username"`
	Password  string   `yaml:"password"`
}

// APIOpenFunc defines a function that opens the api connection
// and returns the defined interface.
type APIOpenFunc func(*api.Info, api.DialOpts) (APIConnection, error)

// GetUserManagerFunc defines a function that takes an api connection
// and returns the (locally defined) UserManager interface.
type GetUserManagerFunc func(conn APIConnection) (UserManager, error)

// LoginCommand logs in to a Juju system and caches the connection
// information.
type LoginCommand struct {
	cmd.CommandBase
	apiOpen        APIOpenFunc
	getUserManager GetUserManagerFunc
	// TODO (thumper): when we support local cert definitions
	// allow the use to specify the user and server address.
	// user      string
	// address   string
	Server      cmd.FileVar
	Name        string
	NewPassword bool
}

var loginDoc = `TODO: add more documentation...`

// Info implements Command.Info
func (c *LoginCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name: "login",
		// TODO(thumper): support user and address options
		// Args: "<name> [<server address>[:<server port>]]"
		Args:    "<name>",
		Purpose: "login to a Juju System",
		Doc:     loginDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *LoginCommand) SetFlags(f *gnuflag.FlagSet) {
	f.Var(&c.Server, "server", "path to yaml-formatted server file")
	f.BoolVar(&c.NewPassword, "new-password", false, "generate a new random password")
}

// SetFlags implements Command.Init.
func (c *LoginCommand) Init(args []string) error {
	if c.apiOpen == nil {
		c.apiOpen = apiOpen
	}
	if c.getUserManager == nil {
		c.getUserManager = getUserManager
	}
	if len(args) == 0 {
		return errors.New("no name specified")
	}

	c.Name, args = args[0], args[1:]
	return cmd.CheckEmpty(args)
}

// Run implements Command.Run
func (c *LoginCommand) Run(ctx *cmd.Context) error {
	// TODO(thumper): as we support the user and address
	// change this check here.
	if c.Server.Path == "" {
		return errors.New("no server file specified")
	}

	serverYAML, err := c.Server.Read(ctx)
	if err != nil {
		return errors.Trace(err)
	}

	var serverDetails ServerFile
	if err := goyaml.Unmarshal(serverYAML, &serverDetails); err != nil {
		return errors.Trace(err)
	}

	// Construct the api.Info struct from the provided values
	// and attempt to connect to the remote server before we do anything else.
	if !names.IsValidUser(serverDetails.Username) {
		return errors.Errorf("%q is not a valid username", serverDetails.Username)
	}

	userTag := names.NewUserTag(serverDetails.Username)
	if userTag.Provider() != names.LocalProvider && c.NewPassword {
		return errors.Errorf("changing passwords is not supported for non-local users")
	}

	var info api.Info
	info.Addrs = serverDetails.Addresses
	info.CACert = serverDetails.CACert
	info.Tag = userTag
	info.Password = serverDetails.Password

	apiState, err := c.apiOpen(&info, api.DefaultDialOpts())
	if err != nil {
		return errors.Trace(err)
	}
	defer apiState.Close()

	// If we get to here, the credentials supplied were sufficient to connect
	// to the Juju System and login. Now we cache the details.
	serverInfo, err := c.cacheConnectionInfo(serverDetails, apiState)
	if err != nil {
		return errors.Trace(err)
	}

	// If we get to here, we have been able to connect to the API server, and
	// also have been able to write the cached information. Now we can change
	// the user's password to a new randomly generated strong password, and
	// update the cached information knowing that the likelihood of failure is
	// minimal.
	if c.NewPassword {
		if err := c.updatePassword(ctx, apiState, userTag, serverInfo); err != nil {
			return errors.Trace(err)
		}
	}

	if err := envcmd.WriteCurrentSystem(c.Name); err != nil {
		return errors.Trace(err)
	}

	return nil
}

func (c *LoginCommand) cacheConnectionInfo(serverDetails ServerFile, apiState APIConnection) (configstore.EnvironInfo, error) {
	store, err := configstore.Default()
	if err != nil {
		return nil, errors.Trace(err)
	}
	serverInfo := store.CreateInfo(c.Name)

	serverTag, err := apiState.ServerTag()
	if err != nil {
		return nil, errors.Wrap(err, errors.New("juju system too old to support login"))
	}

	connectedAddresses, err := network.ParseHostPorts(apiState.Addr())
	if err != nil {
		// Should never happen, since we've just connected with it.
		return nil, errors.Annotatef(err, "invalid API address %q", apiState.Addr())
	}
	addressConnectedTo := connectedAddresses[0]

	addrs, hosts, changed := juju.PrepareEndpointsForCaching(serverInfo, apiState.APIHostPorts(), addressConnectedTo)
	if !changed {
		logger.Infof("api addresses: %v", apiState.APIHostPorts())
		logger.Infof("address connected to: %v", addressConnectedTo)
		return nil, errors.New("no addresses returned from prepare for caching")
	}

	serverInfo.SetAPICredentials(
		configstore.APICredentials{
			User:     serverDetails.Username,
			Password: serverDetails.Password,
		})

	serverInfo.SetAPIEndpoint(configstore.APIEndpoint{
		Addresses:  addrs,
		Hostnames:  hosts,
		CACert:     serverDetails.CACert,
		ServerUUID: serverTag.Id(),
	})

	if err = serverInfo.Write(); err != nil {
		return nil, errors.Trace(err)
	}
	return serverInfo, nil
}

func (c *LoginCommand) updatePassword(ctx *cmd.Context, conn APIConnection, userTag names.UserTag, serverInfo configstore.EnvironInfo) error {
	password, err := utils.RandomPassword()
	if err != nil {
		return errors.Annotate(err, "failed to generate random password")
	}

	userManager, err := c.getUserManager(conn)
	if err != nil {
		return errors.Trace(err)
	}
	if err := userManager.SetPassword(userTag.Name(), password); err != nil {
		errors.Trace(err)
	}
	ctx.Infof("password updated\n")
	creds := serverInfo.APICredentials()
	creds.Password = password
	serverInfo.SetAPICredentials(creds)
	if err = serverInfo.Write(); err != nil {
		return errors.Trace(err)
	}
	return nil
}

type APIConnection interface {
	Close() error
	Addr() string
	APIHostPorts() [][]network.HostPort
	ServerTag() (names.EnvironTag, error)
}

func apiOpen(info *api.Info, opts api.DialOpts) (APIConnection, error) {
	return api.Open(info, opts)
}

type UserManager interface {
	SetPassword(username, password string) error
}

func getUserManager(conn APIConnection) (UserManager, error) {
	if apiState, ok := conn.(*api.State); ok {
		return usermanager.NewClient(apiState), nil
	}
	return nil, errors.Errorf("expected real api connection, got %T", conn)
}
