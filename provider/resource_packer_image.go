package provider

import (
	"context"
	"os"

	"terraform-provider-packer/packer_interop"

	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/pkg/errors"
	"github.com/toowoxx/go-lib-userspace-common/cmds"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type resourceImageType struct {
	ID                types.String      `tfsdk:"id"`
	Variables         map[string]string `tfsdk:"variables"`
	AdditionalParams  []string          `tfsdk:"additional_params"`
	Directory         types.String      `tfsdk:"directory"`
	File              types.String      `tfsdk:"file"`
	Environment       map[string]string `tfsdk:"environment"`
	IgnoreEnvironment types.Bool        `tfsdk:"ignore_environment"`
	Triggers          map[string]string `tfsdk:"triggers"`
	Force             types.Bool        `tfsdk:"force"`
	BuildUUID         types.String      `tfsdk:"build_uuid"`
	Name              types.String      `tfsdk:"name"`
}

func (r resourceImageType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Attributes: map[string]tfsdk.Attribute{
			"id": {
				Type:     types.StringType,
				Computed: true,
			},
			"name": {
				Description: "Name of this build. This value is not passed to Packer.",
				Type:        types.StringType,
				Optional:    true,
			},
			"variables": {
				Description: "Variables to pass to Packer",
				Type:        types.MapType{ElemType: types.StringType},
				Optional:    true,
			},
			"additional_params": {
				Description: "Additional parameters to pass to Packer",
				Type:        types.SetType{ElemType: types.StringType},
				Optional:    true,
			},
			"directory": {
				Description: "Working directory to run Packer inside. Default is cwd.",
				Type:        types.StringType,
				Optional:    true,
			},
			"file": {
				Description: "Packer file to use for building",
				Type:        types.StringType,
				Optional:    true,
			},
			"force": {
				Description: "Force overwriting existing images",
				Type:        types.BoolType,
				Optional:    true,
			},
			"environment": {
				Description: "Environment variables",
				Type:        types.MapType{ElemType: types.StringType},
				Optional:    true,
			},
			"ignore_environment": {
				Description: "Prevents passing all environment variables of the provider through to Packer",
				Type:        types.BoolType,
				Optional:    true,
			},
			"triggers": {
				Description: "Values that, when changed, trigger an update of this resource",
				Type:        types.MapType{ElemType: types.StringType},
				Optional:    true,
			},
			"build_uuid": {
				Description: "UUID that is updated whenever the build has finished. This allows detecting changes.",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

func (r resourceImageType) NewResource(_ context.Context, p provider.Provider) (resource.Resource, diag.Diagnostics) {
	return resourceImage{
		p: *(p.(*tfProvider)),
	}, nil
}

type resourceImage struct {
	p tfProvider
}

func (r resourceImage) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Empty().AtName("id"), req, resp)
}

func (r resourceImage) getDir(dir types.String) string {
	dirVal := dir.Value
	if dir.Unknown || len(dirVal) == 0 {
		dirVal = "."
	}
	return dirVal
}

func (r resourceImage) getFileParam(resourceState *resourceImageType) string {
	if resourceState.File.Null || len(resourceState.File.Value) == 0 {
		return "."
	} else {
		return resourceState.File.Value
	}
}

func (r resourceImage) packerInit(resourceState *resourceImageType) error {
	envVars := packer_interop.EnvVars(resourceState.Environment, !resourceState.IgnoreEnvironment.Value)

	params := []string{"init"}
	params = append(params, r.getFileParam(resourceState))

	exe, _ := os.Executable()
	output, err := cmds.RunCommandInDirWithEnvReturnOutput(exe, r.getDir(resourceState.Directory), envVars, params...)

	if err != nil {
		return errors.Wrap(err, "could not run packer command; output: "+string(output))
	}

	return nil
}

func (r resourceImage) packerBuild(resourceState *resourceImageType) error {
	envVars := packer_interop.EnvVars(resourceState.Environment, !resourceState.IgnoreEnvironment.Value)

	params := []string{"build"}
	for key, value := range resourceState.Variables {
		params = append(params, "-var", key+"="+value)
	}
	if resourceState.Force.Value {
		params = append(params, "-force")
	}
	params = append(params, r.getFileParam(resourceState))
	params = append(params, resourceState.AdditionalParams...)

	exe, _ := os.Executable()
	output, err := cmds.RunCommandInDirWithEnvReturnOutput(exe, r.getDir(resourceState.Directory), envVars, params...)
	if err != nil {
		return errors.Wrap(err, "could not run packer command; output: "+string(output))
	}

	return nil
}

func (r resourceImage) updateState(resourceState *resourceImageType) error {
	if resourceState.ID.Unknown {
		resourceState.ID = types.String{Value: uuid.Must(uuid.NewRandom()).String()}
	}
	resourceState.BuildUUID = types.String{Value: uuid.Must(uuid.NewRandom()).String()}

	return nil
}

func (r resourceImage) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resourceState := resourceImageType{}
	diags := req.Config.Get(ctx, &resourceState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.packerInit(&resourceState)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer init", err.Error())
		return
	}
	err = r.packerBuild(&resourceState)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer build", err.Error())
		return
	}
	err = r.updateState(&resourceState)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer", err.Error())
		return
	}

	diags = resp.State.Set(ctx, &resourceState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceImage) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var resourceState resourceImageType
	diags := req.State.Get(ctx, &resourceState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, &resourceState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceImage) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan resourceImageType
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var resourceState resourceImageType
	diags = req.State.Get(ctx, &resourceState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.packerInit(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer init", err.Error())
		return
	}
	err = r.packerBuild(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer build", err.Error())
		return
	}
	err = r.updateState(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run packer", err.Error())
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceImage) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state resourceImageType
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}
