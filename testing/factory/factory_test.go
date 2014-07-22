// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package factory_test

import (
	"fmt"

	"github.com/juju/charm"
	jtesting "github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "launchpad.net/gocheck"

	"github.com/juju/juju/environmentserver/authentication"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/state"
	statetesting "github.com/juju/juju/state/testing"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/testing/factory"
)

type factorySuite struct {
	testing.BaseSuite
	jtesting.MgoSuite
	State   *state.State
	Factory *factory.Factory
}

var _ = gc.Suite(&factorySuite{})

func (s *factorySuite) SetUpSuite(c *gc.C) {
	s.BaseSuite.SetUpSuite(c)
	s.MgoSuite.SetUpSuite(c)
}

func (s *factorySuite) TearDownSuite(c *gc.C) {
	s.MgoSuite.TearDownSuite(c)
	s.BaseSuite.TearDownSuite(c)
}

func (s *factorySuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.MgoSuite.SetUpTest(c)
	policy := statetesting.MockPolicy{}

	info := &authentication.MongoInfo{
		Info: mongo.Info{
			Addrs:  []string{jtesting.MgoServer.Addr()},
			CACert: testing.CACert,
		},
	}
	opts := mongo.DialOpts{
		Timeout: testing.LongWait,
	}
	cfg := testing.EnvironConfig(c)
	st, err := state.Initialize(info, cfg, opts, &policy)
	c.Assert(err, gc.IsNil)
	s.State = st
	s.Factory = factory.NewFactory(s.State, c)
}

func (s *factorySuite) TearDownTest(c *gc.C) {
	if s.State != nil {
		s.State.Close()
	}
	s.MgoSuite.TearDownTest(c)
	s.BaseSuite.TearDownTest(c)
}

func (s *factorySuite) TestMakeUserAny(c *gc.C) {
	user := s.Factory.MakeAnyUser()
	c.Assert(user.IsDeactivated(), jc.IsFalse)

	saved, err := s.State.User(user.Name())
	c.Assert(err, gc.IsNil)
	c.Assert(saved.Tag(), gc.Equals, user.Tag())
	c.Assert(saved.Name(), gc.Equals, user.Name())
	c.Assert(saved.DisplayName(), gc.Equals, user.DisplayName())
	c.Assert(saved.CreatedBy(), gc.Equals, user.CreatedBy())
	c.Assert(saved.DateCreated(), gc.Equals, user.DateCreated())
	c.Assert(saved.LastConnection(), gc.Equals, user.LastConnection())
	c.Assert(saved.IsDeactivated(), gc.Equals, user.IsDeactivated())
}

func (s *factorySuite) TestMakeUserParams(c *gc.C) {
	username := "bob"
	displayName := "Bob the Builder"
	creator := "eric"
	password := "sekrit"
	user := s.Factory.MakeUser(factory.UserParams{
		Username:    username,
		DisplayName: displayName,
		Creator:     creator,
		Password:    password,
	})
	c.Assert(user.IsDeactivated(), jc.IsFalse)
	c.Assert(user.Name(), gc.Equals, username)
	c.Assert(user.DisplayName(), gc.Equals, displayName)
	c.Assert(user.CreatedBy(), gc.Equals, creator)
	c.Assert(user.PasswordValid(password), jc.IsTrue)

	saved, err := s.State.User(user.Name())
	c.Assert(err, gc.IsNil)
	c.Assert(saved.Tag(), gc.Equals, user.Tag())
	c.Assert(saved.Name(), gc.Equals, user.Name())
	c.Assert(saved.DisplayName(), gc.Equals, user.DisplayName())
	c.Assert(saved.CreatedBy(), gc.Equals, user.CreatedBy())
	c.Assert(saved.DateCreated(), gc.Equals, user.DateCreated())
	c.Assert(saved.LastConnection(), gc.Equals, user.LastConnection())
	c.Assert(saved.IsDeactivated(), gc.Equals, user.IsDeactivated())
}

func (s *factorySuite) TestMakeMachineAny(c *gc.C) {
	machine := s.Factory.MakeAnyMachine()
	c.Assert(machine, gc.NotNil)

	saved, err := s.State.Machine(machine.Id())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Series(), gc.Equals, machine.Series())
	c.Assert(saved.Id(), gc.Equals, machine.Id())
	c.Assert(saved.Series(), gc.Equals, machine.Series())
	c.Assert(saved.Tag(), gc.Equals, machine.Tag())
	c.Assert(saved.Life(), gc.Equals, machine.Life())
	c.Assert(saved.Jobs(), gc.Equals, machine.Jobs())
	savedInstanceId, err := saved.InstanceId()
	c.Assert(err, gc.IsNil)
	machineInstanceId, err := machine.InstanceId()
	c.Assert(err, gc.IsNil)
	c.Assert(savedInstanceId, gc.Equals, machineInstanceId)
	c.Assert(saved.Clean(), gc.Equals, machine.Clean())
}

func (s *factorySuite) TestMakeMachine(c *gc.C) {
	series := "precise"
	jobs := []state.MachineJob{state.JobHostUnits}
	password := "some-password"
	nonce := "some-nonce"
	id := instance.Id("some-id")

	machine := s.Factory.MakeMachine(factory.MachineParams{
		Series:   series,
		Jobs:     jobs,
		Password: password,
		Nonce:    nonce,
		Id:       id,
	})
	c.Assert(machine, gc.NotNil)

	c.Assert(machine.Series(), gc.Equals, series)
	c.Assert(machine.Jobs, gc.Equals, jobs)
	machineInstanceId, err := machine.InstanceId()
	c.Assert(err, gc.IsNil)
	c.Assert(machineInstanceId, gc.Equals, id)
	c.Assert(machine.CheckProvisioned(nonce), gc.Equals, true)
	c.Assert(machine.PasswordValid(password), gc.Equals, true)

	saved, err := s.State.Machine(machine.Id())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Id(), gc.Equals, machine.Id())
	c.Assert(saved.Series(), gc.Equals, machine.Series())
	c.Assert(saved.Tag(), gc.Equals, machine.Tag())
	c.Assert(saved.Life(), gc.Equals, machine.Life())
	c.Assert(saved.Jobs(), gc.Equals, machine.Jobs())
	savedInstanceId, err := saved.InstanceId()
	c.Assert(err, gc.IsNil)
	c.Assert(savedInstanceId, gc.Equals, machineInstanceId)
	c.Assert(saved.Clean(), gc.Equals, machine.Clean())
}

func (s *factorySuite) TestMakeCharmAny(c *gc.C) {
	charm := s.Factory.MakeAnyCharm()
	c.Assert(charm, gc.NotNil)

	saved, err := s.State.Charm(charm.URL())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.URL(), gc.DeepEquals, charm.URL())
	c.Assert(saved.Meta(), gc.DeepEquals, charm.Meta())
	c.Assert(saved.BundleURL(), gc.DeepEquals, charm.BundleURL())
	c.Assert(saved.BundleSha256(), gc.Equals, charm.BundleSha256())
}

func (s *factorySuite) TestMakeCharm(c *gc.C) {
	series := "quantal"
	name := "mysql"
	revision := 13
	url := fmt.Sprintf("cs:%s/%s-%d", series, name, revision)
	ch := s.Factory.MakeCharm(factory.CharmParams{
		URL: url,
	})
	c.Assert(ch, gc.NotNil)

	c.Assert(ch.URL(), gc.DeepEquals, charm.MustParseURL(url))

	saved, err := s.State.Charm(ch.URL())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.URL(), gc.DeepEquals, ch.URL())
	c.Assert(saved.Meta(), gc.DeepEquals, ch.Meta())
	c.Assert(saved.BundleURL(), gc.DeepEquals, ch.BundleURL())
	c.Assert(saved.BundleSha256(), gc.Equals, ch.BundleSha256())
}

func (s *factorySuite) TestMakeServiceAny(c *gc.C) {
	service := s.Factory.MakeAnyService()
	c.Assert(service, gc.NotNil)

	saved, err := s.State.Service(service.Name())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Name(), gc.Equals, service.Name())
	c.Assert(saved.Tag(), gc.Equals, service.Tag())
	c.Assert(saved.Life(), gc.Equals, service.Life())
}

func (s *factorySuite) TestMakeService(c *gc.C) {
	name := "servicename"
	charm := s.Factory.MakeAnyCharm()
	creator := "user-bill"

	service := s.Factory.MakeService(factory.ServiceParams{
		Name:    name,
		Charm:   charm,
		Creator: creator,
	})
	c.Assert(service, gc.NotNil)

	c.Assert(service.Name(), gc.Equals, name)
	c.Assert(service.GetOwnerTag(), gc.Equals, creator)
	curl, _ := service.CharmURL()
	c.Assert(curl, gc.Equals, charm.URL())

	saved, err := s.State.Service(service.Name())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Name(), gc.Equals, service.Name())
	c.Assert(saved.Tag(), gc.Equals, service.Tag())
	c.Assert(saved.Life(), gc.Equals, service.Life())
}

func (s *factorySuite) TestMakeUnitAny(c *gc.C) {
	unit := s.Factory.MakeAnyUnit()
	c.Assert(unit, gc.NotNil)

	saved, err := s.State.Unit(unit.Name())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Name(), gc.Equals, unit.Name())
	c.Assert(saved.ServiceName(), gc.Equals, unit.ServiceName())
	c.Assert(saved.Series(), gc.Equals, unit.Series())
	c.Assert(saved.Life(), gc.Equals, unit.Life())
}

func (s *factorySuite) TestMakeUnit(c *gc.C) {
	service := s.Factory.MakeAnyService()
	unit := s.Factory.MakeUnit(factory.UnitParams{
		Service: service,
	})
	c.Assert(unit, gc.NotNil)

	c.Assert(unit.ServiceName(), gc.Equals, service.Name())

	saved, err := s.State.Unit(unit.Name())
	c.Assert(err, gc.IsNil)

	c.Assert(saved.Name(), gc.Equals, unit.Name())
	c.Assert(saved.ServiceName(), gc.Equals, unit.ServiceName())
	c.Assert(saved.Series(), gc.Equals, unit.Series())
	c.Assert(saved.Life(), gc.Equals, unit.Life())
}
