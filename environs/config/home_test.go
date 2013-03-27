package config_test

import (
	. "launchpad.net/gocheck"
	"path/filepath"

	"launchpad.net/juju-core/environs/config"
)

type JujuHomeSuite struct {
	jujuHome string
}

var _ = Suite(&JujuHomeSuite{})

func (s *JujuHomeSuite) TestStandardHome(c *C) {
	testJujuHome := c.MkDir()
	defer config.SetJujuHome(config.SetJujuHome(testJujuHome))
	c.Assert(config.JujuHome(), Equals, testJujuHome)
}

func (s *JujuHomeSuite) TestErrorHome(c *C) {
	defer config.SetJujuHome(config.SetJujuHome(""))
	// Invalid juju home leads to panic when retrieving.
	f := func() { _ = config.JujuHome() }
	c.Assert(f, PanicMatches, "juju home hasn't been initialized")
}

func (s *JujuHomeSuite) TestHomePath(c *C) {
	testJujuHome := c.MkDir()
	defer config.SetJujuHome(config.SetJujuHome(testJujuHome))
	envPath := config.JujuHomePath("environments.yaml")
	c.Assert(envPath, Equals, filepath.Join(testJujuHome, "environments.yaml"))
}
