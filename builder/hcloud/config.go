// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:generate packer-sdc mapstructure-to-hcl2 -type Config,imageFilter

package hcloud

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"github.com/hashicorp/packer-plugin-sdk/uuid"
	"github.com/mitchellh/mapstructure"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	Comm                communicator.Config `mapstructure:",squash"`

	HCloudToken string `mapstructure:"token"`
	Endpoint    string `mapstructure:"endpoint"`

	PollInterval time.Duration `mapstructure:"poll_interval"`

	ServerName        string            `mapstructure:"server_name"`
	Location          string            `mapstructure:"location"`
	ServerType        string            `mapstructure:"server_type"`
	ServerLabels      map[string]string `mapstructure:"server_labels"`
	UpgradeServerType string            `mapstructure:"upgrade_server_type"`
	Image             string            `mapstructure:"image"`
	ImageFilter       *imageFilter      `mapstructure:"image_filter"`

	SnapshotName   string            `mapstructure:"snapshot_name"`
	SnapshotLabels map[string]string `mapstructure:"snapshot_labels"`
	UserData       string            `mapstructure:"user_data"`
	UserDataFile   string            `mapstructure:"user_data_file"`
	SSHKeys        []string          `mapstructure:"ssh_keys"`
	SSHKeysLabels  map[string]string `mapstructure:"ssh_keys_labels"`

	Networks           []int64  `mapstructure:"networks"`
	PublicIPv4         string   `mapstructure:"public_ipv4"`
	PublicIPv4Disabled bool     `mapstructure:"public_ipv4_disabled"`
	PublicIPv6         string   `mapstructure:"public_ipv6"`
	PublicIPv6Disabled bool     `mapstructure:"public_ipv6_disabled"`
	Firewalls          []string `mapstructure:"firewalls"`
	Volumes            []string `mapstructure:"volumes"`

	RescueMode string `mapstructure:"rescue"`

	KeepServer bool     `mapstructure:"keep_server"`
	SkipSnapshot bool     `mapstructure:"skip_snapshot"`

	ctx interpolate.Context
}

type imageFilter struct {
	WithSelector []string `mapstructure:"with_selector"`
	MostRecent   bool     `mapstructure:"most_recent"`
}

func (c *Config) Prepare(raws ...interface{}) ([]string, error) {
	var md mapstructure.Metadata
	err := config.Decode(c, &config.DecodeOpts{
		Metadata:           &md,
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{
				"run_command",
			},
		},
	}, raws...)
	if err != nil {
		return nil, err
	}

	// Defaults
	if c.HCloudToken == "" {
		c.HCloudToken = os.Getenv("HCLOUD_TOKEN")
	}
	if c.Endpoint == "" {
		if os.Getenv("HCLOUD_ENDPOINT") != "" {
			c.Endpoint = os.Getenv("HCLOUD_ENDPOINT")
		} else {
			c.Endpoint = hcloud.Endpoint
		}
	}
	if c.PollInterval == 0 {
		c.PollInterval = 500 * time.Millisecond
	}

	if c.SnapshotName == "" {
		def, err := interpolate.Render("packer-{{timestamp}}", nil)
		if err != nil {
			panic(err)
		}
		// Default to packer-{{ unix timestamp (utc) }}
		c.SnapshotName = def
	}

	if c.ServerName == "" {
		// Default to packer-[time-ordered-uuid]
		c.ServerName = fmt.Sprintf("packer-%s", uuid.TimeOrderedUUID())
	}

	var errs *packersdk.MultiError
	if es := c.Comm.Prepare(&c.ctx); len(es) > 0 {
		errs = packersdk.MultiErrorAppend(errs, es...)
	}
	if c.HCloudToken == "" {
		// Required configurations that will display errors if not set
		errs = packersdk.MultiErrorAppend(
			errs, errors.New("token is missing, make sure to configure your Hetzner Cloud token"))
	}

	if c.Location == "" {
		errs = packersdk.MultiErrorAppend(
			errs, errors.New("location is required"))
	}

	if c.ServerType == "" {
		errs = packersdk.MultiErrorAppend(
			errs, errors.New("server type is required"))
	}

	if c.Image == "" && c.ImageFilter == nil {
		errs = packersdk.MultiErrorAppend(
			errs, errors.New("image or image_filter is required"))
	}
	if c.ImageFilter != nil {
		if len(c.ImageFilter.WithSelector) == 0 {
			errs = packersdk.MultiErrorAppend(
				errs, errors.New("image_filter.with_selector is required when specifying filter"))
		} else if c.Image != "" {
			errs = packersdk.MultiErrorAppend(
				errs, errors.New("only one of image or image_filter can be specified"))
		}
	}

	if c.UserData != "" && c.UserDataFile != "" {
		errs = packersdk.MultiErrorAppend(
			errs, errors.New("only one of user_data or user_data_file can be specified"))
	} else if c.UserDataFile != "" {
		if _, err := os.Stat(c.UserDataFile); err != nil {
			errs = packersdk.MultiErrorAppend(
				errs, fmt.Errorf("user_data_file not found: %s", c.UserDataFile))
		}
	}

	if errs != nil && len(errs.Errors) > 0 {
		return nil, errs
	}

	packersdk.LogSecretFilter.Set(c.HCloudToken)
	return nil, nil
}

func getServerIP(state multistep.StateBag) (string, error) {
	return state.Get(StateServerIP).(string), nil
}
