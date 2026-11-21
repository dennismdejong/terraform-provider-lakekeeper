package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

var (
	_ datasource.DataSource              = &lakekeeperGenericTableDataSource{}
	_ datasource.DataSourceWithConfigure = &lakekeeperGenericTableDataSource{}
)

func init() { registerDataSource(NewLakekeeperGenericTableDataSource) }

// NewLakekeeperGenericTableDataSource reads a generic table without managing
// its lifecycle.
func NewLakekeeperGenericTableDataSource() datasource.DataSource {
	return &lakekeeperGenericTableDataSource{}
}

type lakekeeperGenericTableDataSource struct{ client *api.Client }

type lakekeeperGenericTableDataSourceModel struct {
	ID             types.String         `tfsdk:"id"`
	ProjectID      types.String         `tfsdk:"project_id"`
	WarehouseID    types.String         `tfsdk:"warehouse_id"`
	Namespace      types.String         `tfsdk:"namespace"`
	Name           types.String         `tfsdk:"name"`
	GenericTableID types.String         `tfsdk:"generic_table_id"`
	Format         types.String         `tfsdk:"format"`
	BaseLocation   types.String         `tfsdk:"base_location"`
	Doc            types.String         `tfsdk:"doc"`
	Properties     types.Map            `tfsdk:"properties"`
	Schema         jsontypes.Normalized `tfsdk:"schema"`
	Protected      types.Bool           `tfsdk:"protected"`
}

func (d *lakekeeperGenericTableDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_generic_table"
}

func (d *lakekeeperGenericTableDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read an existing generic (non-Iceberg) table by its project, warehouse UUID and dotted namespace path. Verifies the warehouse belongs to the project. No resources or permissions are changed.\n\n" +
			"**Upstream API**: [Lakekeeper REST API docs](https://docs.lakekeeper.io/docs/nightly/api/generic-table/#tag/generic-table/operation/load_generic_table)",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Composite `project_id/warehouse_id/namespace/name` identity."},
			"project_id":   schema.StringAttribute{Required: true, MarkdownDescription: "Project UUID."},
			"warehouse_id": schema.StringAttribute{Required: true, MarkdownDescription: "Warehouse UUID; names are not used to select warehouses."},
			"namespace":    schema.StringAttribute{Required: true, MarkdownDescription: "Dotted namespace path, including all parent levels."},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the generic table.",
				Validators: []validator.String{
					stringvalidator.NoneOf(".", ".."),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[^/\x1f]+$`), "must not contain '/' or the multi-level separator"),
				},
			},
			"generic_table_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Generic table UUID used by the protection and permission APIs."},
			"format":           schema.StringAttribute{Computed: true, MarkdownDescription: "Table format, for example `lance`, `delta` or `dataset`."},
			"base_location":    schema.StringAttribute{Computed: true, MarkdownDescription: "Base location of the table."},
			"doc":              schema.StringAttribute{Computed: true, MarkdownDescription: "Free-form description, if set."},
			"properties":       schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "String properties stored with the table."},
			"schema":           schema.StringAttribute{Computed: true, CustomType: jsontypes.NormalizedType{}, MarkdownDescription: "Table schema as a JSON document."},
			"protected":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the table is protected from being dropped."},
		},
	}
}

func (d *lakekeeperGenericTableDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData != nil {
		d.client = req.ProviderData.(*LakekeeperDatasourceData).Client
	}
}

func (d *lakekeeperGenericTableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state lakekeeperGenericTableDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	namespace, err := splitNamespacePath(state.Namespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid namespace", err.Error())
		return
	}
	if err := verifyWarehouse(ctx, d.client, state.ProjectID.ValueString(), state.WarehouseID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot read generic table", err.Error())
		return
	}

	table, err := d.client.LoadGenericTable(ctx, state.ProjectID.ValueString(), state.WarehouseID.ValueString(), namespace, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read generic table", err.Error())
		return
	}
	id, err := d.client.ResolveGenericTableID(ctx, state.ProjectID.ValueString(), state.WarehouseID.ValueString(), namespace, table.Name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read generic table", err.Error())
		return
	}

	state.ID = types.StringValue(fmt.Sprintf("%s/%s/%s/%s", state.ProjectID.ValueString(), state.WarehouseID.ValueString(), state.Namespace.ValueString(), table.Name))
	state.Name = types.StringValue(table.Name)
	state.GenericTableID = types.StringValue(id)
	state.Format = types.StringValue(table.Format)
	state.BaseLocation = types.StringValue(table.BaseLocation)
	state.Protected = types.BoolValue(table.Protected)

	if table.Doc == nil {
		state.Doc = types.StringNull()
	} else {
		state.Doc = types.StringValue(*table.Doc)
	}
	if table.Properties == nil {
		// Lakekeeper returns no properties for an empty map. Report an empty
		// map so a configured `properties = {}` does not plan a replacement.
		state.Properties = types.MapValueMust(types.StringType, map[string]attr.Value{})
	} else {
		properties, diags := types.MapValueFrom(ctx, types.StringType, table.Properties)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Properties = properties
	}
	if table.Schema == nil {
		state.Schema = jsontypes.NewNormalizedNull()
	} else {
		state.Schema = jsontypes.NewNormalizedValue(canonicalJSON(table.Schema))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
