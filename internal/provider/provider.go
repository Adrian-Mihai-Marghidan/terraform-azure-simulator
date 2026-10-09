package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-azure-simulator/internal/store"
)

const Version = "0.1.1"

var _ tfprovider.Provider = &SimulatorProvider{}
var _ resource.Resource = &ResourceGroup{}
var _ resource.ResourceWithConfigure = &ResourceGroup{}
var _ resource.ResourceWithValidateConfig = &ResourceGroup{}

type SimulatorProvider struct{}
type providerModel struct {
	StoragePath types.String `tfsdk:"storage_path"`
}

func New() tfprovider.Provider { return &SimulatorProvider{} }

func (p *SimulatorProvider) Metadata(_ context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "azuresim"
	resp.Version = Version
}

func (p *SimulatorProvider) Schema(_ context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Description: "Local Azure-style Resource Group simulator. No Azure authentication, SDK, or cloud operations.",
		Attributes: map[string]providerschema.Attribute{
			"storage_path": providerschema.StringAttribute{
				Required:    true,
				Description: "Absolute path to persistent simulated infrastructure JSON, separate from Terraform state.",
			},
		},
	}
}

func (p *SimulatorProvider) Configure(ctx context.Context, req tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.StoragePath.IsNull() || config.StoragePath.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("storage_path"), "Simulator storage path unavailable", "Set storage_path to a known absolute path before planning.")
		return
	}
	s, err := store.New(config.StoragePath.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("storage_path"), "Invalid simulator storage path", err.Error())
		return
	}
	resp.ResourceData = s
}

func (p *SimulatorProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &ResourceGroup{} }}
}

func (p *SimulatorProvider) DataSources(_ context.Context) []func() datasource.DataSource { return nil }

type ResourceGroup struct{ storage *store.Store }
type resourceGroupModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Location types.String `tfsdk:"location"`
	Tags     types.Map    `tfsdk:"tags"`
}

func (r *ResourceGroup) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_group"
}

func (r *ResourceGroup) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Simulated Resource Group. Name/location changes require replacement; tags update in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"location": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"tags": schema.MapAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})),
			},
		},
	}
}

func (r *ResourceGroup) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config resourceGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Name.IsUnknown() && !config.Location.IsUnknown() && !config.Name.IsNull() && !config.Location.IsNull() {
		if err := store.Validate(config.Name.ValueString(), config.Location.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid simulated Resource Group", err.Error())
		}
	}
}

func (r *ResourceGroup) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	} // terraform validate is offline.
	s, ok := req.ProviderData.(*store.Store)
	if !ok {
		resp.Diagnostics.AddError("Unexpected simulator configuration", fmt.Sprintf("Expected *store.Store, got %T", req.ProviderData))
		return
	}
	r.storage = s
}

func (r *ResourceGroup) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model resourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tags := map[string]string{}
	resp.Diagnostics.Append(model.Tags.ElementsAs(ctx, &tags, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group := store.ResourceGroup{Name: model.Name.ValueString(), Location: model.Location.ValueString(), Tags: tags}
	if err := r.storage.Create(ctx, group); err != nil {
		resp.Diagnostics.AddError("Cannot create simulated Resource Group", err.Error())
		return
	}
	model.ID = types.StringValue(store.ID(group.Name))
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ResourceGroup) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model resourceGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := r.storage.Read(ctx, model.ID.ValueString())
	if errors.Is(err, store.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read simulated Resource Group", err.Error())
		return
	}
	model.ID = types.StringValue(group.ID)
	model.Name = types.StringValue(group.Name)
	model.Location = types.StringValue(group.Location)
	model.Tags, _ = types.MapValueFrom(ctx, types.StringType, group.Tags)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ResourceGroup) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model resourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tags := map[string]string{}
	resp.Diagnostics.Append(model.Tags.ElementsAs(ctx, &tags, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.storage.SetTags(ctx, model.ID.ValueString(), tags); err != nil {
		resp.Diagnostics.AddError("Cannot update simulated Resource Group", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ResourceGroup) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model resourceGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.storage.Delete(ctx, model.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete simulated Resource Group", err.Error())
	}
}
