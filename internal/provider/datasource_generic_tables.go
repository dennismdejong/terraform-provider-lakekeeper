package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

var (
	_ datasource.DataSource              = &lakekeeperGenericTablesDataSource{}
	_ datasource.DataSourceWithConfigure = &lakekeeperGenericTablesDataSource{}
)

func init() { registerDataSource(NewLakekeeperGenericTablesDataSource) }

// NewLakekeeperGenericTablesDataSource lists the generic tables in a
// namespace without managing them.
func NewLakekeeperGenericTablesDataSource() datasource.DataSource {
	return &lakekeeperGenericTablesDataSource{}
}

type lakekeeperGenericTablesDataSource struct{ client *api.Client }

type genericTableListEntryModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Format    types.String `tfsdk:"format"`
	Namespace types.List   `tfsdk:"namespace"`
}

type lakekeeperGenericTablesDataSourceModel struct {
	ProjectID   types.String                 `tfsdk:"project_id"`
	WarehouseID types.String                 `tfsdk:"warehouse_id"`
	Namespace   types.String                 `tfsdk:"namespace"`
	Tables      []genericTableListEntryModel `tfsdk:"tables"`
}

func (d *lakekeeperGenericTablesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_generic_tables"
}

func (d *lakekeeperGenericTablesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List the generic (non-Iceberg) tables in one namespace, sorted by name. Verifies the warehouse belongs to the project. No resources or permissions are changed.\n\n" +
			"**Upstream API**: [Lakekeeper REST API docs](https://docs.lakekeeper.io/docs/nightly/api/generic-table/#tag/generic-table/operation/list_generic_tables)",
		Attributes: map[string]schema.Attribute{
			"project_id":   schema.StringAttribute{Required: true, MarkdownDescription: "Project UUID."},
			"warehouse_id": schema.StringAttribute{Required: true, MarkdownDescription: "Warehouse UUID; names are not used to select warehouses."},
			"namespace":    schema.StringAttribute{Required: true, MarkdownDescription: "Dotted namespace path, including all parent levels."},
			"tables": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Generic tables in the namespace, sorted by name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Generic table UUID used by the protection and permission APIs."},
						"name":   schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the generic table."},
						"format": schema.StringAttribute{Computed: true, MarkdownDescription: "Table format, for example `lance`, `delta` or `dataset`."},
						"namespace": schema.ListAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Namespace levels of the table.",
						},
					},
				},
			},
		},
	}
}

func (d *lakekeeperGenericTablesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData != nil {
		d.client = req.ProviderData.(*LakekeeperDatasourceData).Client
	}
}

func (d *lakekeeperGenericTablesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state lakekeeperGenericTablesDataSourceModel
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
		resp.Diagnostics.AddError("Cannot list generic tables", err.Error())
		return
	}

	tables, err := d.client.ListGenericTables(ctx, state.ProjectID.ValueString(), state.WarehouseID.ValueString(), namespace)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list generic tables", err.Error())
		return
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })

	state.Tables = make([]genericTableListEntryModel, 0, len(tables))
	for _, table := range tables {
		levels, diags := types.ListValueFrom(ctx, types.StringType, table.Namespace)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Tables = append(state.Tables, genericTableListEntryModel{
			ID:        types.StringValue(table.ID),
			Name:      types.StringValue(table.Name),
			Format:    types.StringValue(table.Format),
			Namespace: levels,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
