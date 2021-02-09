package qingcloud

import (
	"context"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	gossh "golang.org/x/crypto/ssh"
	validator "gopkg.in/asaskevich/govalidator.v8"
	"time"
)

const BuilderId = "yunify.qingcloud"

const (
	BuilderConfig   = "config"
	UI              = "ui"
	InstanceID      = "instancd_id"
	ImageID         = "image_id"
	SecurityGroupID = "security_group_id"
	EIPID           = "eip_id"
	PublicIP        = "public_ip"
	PrivateIP       = "private_ip"
	LoginKeyPairID  = "keypair_id"
	PrivateKey      = "private_key_content"
)

const (
	AllocateNewID = "new"
)

const (
	DefaultPublicKey  = "~/.ssh/id_rsa.pub"
	DefaultPrivateKey = "~/.ssh/id_rsa"
	LocalKey          = "local"
)

var DefaultTimeout = time.Second * 300
var DefaultInterval = time.Second * 5

// Builder qingcloud builder
type Builder struct {
	runner multistep.Runner
	config Config
}

func (builder *Builder) ConfigSpec() hcldec.ObjectSpec {
	return builder.config.FlatMapstructure().HCL2Spec()
}

func (builder *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	c, warnings, errs := NewConfig(raws...)

	if errs != nil {
		return nil, warnings, errs
	}

	builder.config = *c

	return []string{"GeneratedData"}, nil, nil
}

func (builder *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {
	// Setup
	state := new(multistep.BasicStateBag)
	state.Put(BuilderConfig, builder.config)
	state.Put(UI, ui)
	state.Put("hook", hook)

	// Run
	steps := []multistep.Step{
		new(StepEnsureSecurityGroup),
		new(StepEnsureKeypair),
		new(StepCreateVM),
		new(StepEnsureIP),
		&communicator.StepConnect{
			Config:    &builder.config.Config,
			Host:      builder.getHost,
			SSHConfig: builder.getSSHConfig,
		},
		new(commonsteps.StepProvision),
		new(StepShutDownVM),
		new(StepBuildImage),
	}

	builder.runner = commonsteps.NewRunner(steps, builder.config.PackerConfig, ui)
	builder.runner.Run(ctx, state)
	imageID, ok := state.GetOk(ImageID)
	if !ok {
		return nil, fmt.Errorf("Failed to get image id:%v", imageID)
	}

	imageService, _ := builder.config.GetQingCloudService().Image(builder.config.Zone)
	artifact := &ImageArtifact{
		ImageID:      imageID.(string),
		ImageService: imageService,
	}
	return artifact, nil
}

func (builder *Builder) getHost(state multistep.StateBag) (string, error) {
	publicIP, ok := state.Get(PublicIP).(string)
	if ok && validator.IsIP(publicIP) {
		return publicIP, nil
	}
	privateIP, ok := state.Get(PrivateIP).(string)
	if ok && validator.IsIP(privateIP) {
		return privateIP, nil
	}
	return "", fmt.Errorf("neither public ip nor private ip is valid")
}

func (builder *Builder) getSSHConfig(state multistep.StateBag) (*gossh.ClientConfig, error) {
	config := state.Get(BuilderConfig).(Config)
	privateKey := state.Get(PrivateKey).(string)
	signer, err := gossh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to set up ssh config：%v", err)
	}
	return &gossh.ClientConfig{
		User: config.SSHUsername,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}, nil
}
