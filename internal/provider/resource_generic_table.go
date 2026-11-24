package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ resource.Resource                = &lakekeeperGenericTableResource{}
	_ resource.ResourceWithConfigure   = &lakekeeperGenericTableResource{}
	_ resource.ResourceWithImportState = &lakekeeperGenericTableResource{}
)

func init() {
	registerResource(NewLakekeeperGenericTableResource)
}

// NewLakekeeperGenericTableResource is a helper function to simplify the provider implementation.
func NewLakekeeperGenericTableResource() resource.Resource {
	return &lakekeeperGenericTableResource{}
}

type lakekeeperGenericTableResource struct {
	client *api.Client
}

// lakekeeperGenericTableResourceModel describes the resource data model.
type lakekeeperGenericTableResourceModel struct {
	ID             types.String         `tfsdk:"id"`
	ProjectID      types.String         `tfsdk:"project_id"`
	WarehouseName  types.String         `tfsdk:"warehouse_name"`
	WarehouseID    types.String         `tfsdk:"warehouse_id"`
	Namespace      types.String         `tfsdk:"namespace"`
	Name           types.String         `tfsdk:"name"`
	Format         types.String         `tfsdk:"format"`
	BaseLocation   types.String         `tfsdk:"base_location"`
	Doc            types.String         `tfsdk:"doc"`
	Properties     types.Map            `tfsdk:"properties"`
	Schema         jsontypes.Normalized `tfsdk:"schema"`
	Protected      types.Bool           `tfsdk:"protected"`
	GenericTableID types.String         `tfsdk:"generic_table_id"`
	PurgeOnDelete  types.Bool           `tfsdk:"purge_on_delete"`
}

func (r *lakekeeperGenericTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_generic_table"
}

func (r *lakekeeperGenericTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The `lakekeeper_generic_table` resource manages a generic (non-Iceberg) table inside a Lakekeeper namespace. Lakekeeper catalogs the table's identity, location and governance; engines such as Lance and Delta read and write the data directly.\n\n" +
			"Lakekeeper has no update operation for generic tables. Changing `namespace`, `format`, `base_location`, `doc`, `properties` or `schema` replaces the table; changing `name` renames it in place.\n\n" +
			"**Upstream API**: [Lakekeeper REST API docs](https://docs.lakekeeper.io/docs/nightly/api/generic-table/#tag/generic-table/operation/create_generic_table)",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the generic table, in the form `{{project_id}}/{{warehouse_name}}/{{namespace}}/{{name}}`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					keepUnlessChanged{attributes: []string{"project_id", "warehouse_name", "namespace", "name"}},
				},
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The internal ID of the project where the table is located.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"warehouse_name": schema.StringAttribute{
				MarkdownDescription: "The name of the warehouse where the table is located.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"warehouse_id": schema.StringAttribute{
				MarkdownDescription: "The UUID of the warehouse. Use it with the protection and permission APIs.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					keepUnlessChanged{attributes: []string{"project_id", "warehouse_name"}},
				},
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "The dotted path of the namespace, including all parent levels, for example `sales.bronze`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the table. Changing it renames the table in place.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.NoneOf(".", ".."),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[^/\x1f]+$`), "must not contain '/' or the multi-level separator"),
				},
			},
			"format": schema.StringAttribute{
				MarkdownDescription: "The table format, for example `lance`, `delta` or `dataset`. Lakekeeper treats this as an opaque string.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"base_location": schema.StringAttribute{
				MarkdownDescription: "The base location of the table. When omitted, Lakekeeper derives it from the warehouse storage. Lakekeeper stores locations without trailing slashes; the provider treats a trailing slash as equivalent. Changing it replaces the table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					keepUnlessChanged{attributes: []string{"project_id", "warehouse_name", "namespace"}},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"doc": schema.StringAttribute{
				MarkdownDescription: "A free-form description of the table. Changing it replaces the table. On import, omitting it keeps the stored value; set it to an empty string to clear it.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"properties": schema.MapAttribute{
				MarkdownDescription: "Arbitrary string properties stored with the table. Changing them replaces the table. On import, omitting them keeps the stored values; set them to an empty map to clear them.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
					mapplanmodifier.RequiresReplace(),
				},
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "The table schema as a JSON document. Lakekeeper stores it as informational metadata and does not validate it against the data files. Changing it replaces the table. On import, omitting it keeps the stored value.",
				CustomType:          jsontypes.NormalizedType{},
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"protected": schema.BoolAttribute{
				MarkdownDescription: "Whether the table is protected from being dropped. Lakekeeper refuses to drop a protected table, so remove the protection before destroying this resource.",
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"generic_table_id": schema.StringAttribute{
				MarkdownDescription: "The generic table UUID used by the protection and permission APIs.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					keepUnlessChanged{attributes: []string{"project_id", "warehouse_name", "namespace", "format", "base_location", "doc", "properties", "schema"}},
				},
			},
			"purge_on_delete": schema.BoolAttribute{
				MarkdownDescription: "Whether destroying this resource also deletes the files under the table's base location. Defaults to `false`, so a destroy only removes the catalog entry; set it to `true` to delete the data files as well.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (r *lakekeeperGenericTableResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*LakekeeperResourceData).Client
}

func (r *lakekeeperGenericTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lakekeeperGenericTableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	namespace, err := splitNamespacePath(plan.Namespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid namespace", err.Error())
		return
	}

	request := api.GenericTableRequest{
		Name:   plan.Name.ValueString(),
		Format: plan.Format.ValueString(),
	}
	if !plan.BaseLocation.IsNull() && !plan.BaseLocation.IsUnknown() {
		request.BaseLocation = new(plan.BaseLocation.ValueString())
	}
	if !plan.Doc.IsNull() && !plan.Doc.IsUnknown() {
		request.Doc = new(plan.Doc.ValueString())
	}
	if !plan.Properties.IsNull() && !plan.Properties.IsUnknown() {
		properties := map[string]string{}
		resp.Diagnostics.Append(plan.Properties.ElementsAs(ctx, &properties, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		request.Properties = properties
	}
	if !plan.Schema.IsNull() && !plan.Schema.IsUnknown() {
		request.Schema = json.RawMessage(plan.Schema.ValueString())
	}

	warehouseID, err := r.client.LookupWarehouseID(ctx, plan.ProjectID.ValueString(), plan.WarehouseName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot resolve warehouse", err.Error())
		return
	}
	plan.WarehouseID = types.StringValue(warehouseID)

	table, err := r.client.CreateGenericTable(ctx, plan.ProjectID.ValueString(), warehouseID, namespace, request)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create generic table", err.Error())
		return
	}

	// The server may omit doc, properties and schema from the create response
	// when they are empty, and it derives or normalizes base_location. Copy the
	// response so no attribute stays unknown once the table exists.
	if err := setGenericTableMetadata(ctx, &plan, table); err != nil {
		resp.Diagnostics.AddError("Cannot read created generic table", err.Error())
		return
	}
	plan.ID = types.StringValue(genericTableInternalID(plan.ProjectID.ValueString(), plan.WarehouseName.ValueString(), namespace, plan.Name.ValueString()))
	if table.ID == "" {
		plan.GenericTableID = types.StringNull()
	} else {
		plan.GenericTableID = types.StringValue(table.ID)
	}

	tflog.Debug(ctx, "created a generic table", map[string]any{"id": plan.ID.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lakekeeperGenericTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lakekeeperGenericTableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.refreshGenericTable(ctx, &state); err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read generic table", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lakekeeperGenericTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state lakekeeperGenericTableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	namespace, err := splitNamespacePath(state.Namespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid namespace", err.Error())
		return
	}

	warehouseID := state.WarehouseID.ValueString()
	if warehouseID == "" {
		warehouseID, err = r.client.LookupWarehouseID(ctx, state.ProjectID.ValueString(), state.WarehouseName.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Cannot resolve warehouse", err.Error())
			return
		}
	}
	plan.WarehouseID = types.StringValue(warehouseID)

	renamed := plan.Name.ValueString() != state.Name.ValueString()
	if renamed {
		err = r.client.RenameGenericTable(ctx,
			state.ProjectID.ValueString(), warehouseID,
			namespace, state.Name.ValueString(),
			namespace, plan.Name.ValueString(),
		)
		if err != nil {
			resp.Diagnostics.AddError("Cannot rename generic table", err.Error())
			return
		}
	}

	if err := r.refreshGenericTable(ctx, &plan); err != nil {
		if !renamed {
			resp.Diagnostics.AddError("Cannot read generic table", err.Error())
			return
		}
		// The rename succeeded, but the follow-up read failed. Persist the new
		// name from the prior state, whose other values are still valid, so the
		// next refresh retries instead of renaming a name that no longer exists.
		// Starting from state also keeps the State.Set below from receiving
		// unknowns such as the computed `protected`.
		fallback := state
		fallback.Name = plan.Name
		fallback.WarehouseID = types.StringValue(warehouseID)
		fallback.ID = types.StringValue(genericTableInternalID(plan.ProjectID.ValueString(), plan.WarehouseName.ValueString(), namespace, plan.Name.ValueString()))
		plan = fallback
		resp.Diagnostics.AddWarning("Cannot read renamed generic table", fmt.Sprintf("%v; the new name is recorded and the next refresh will retry", err))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lakekeeperGenericTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lakekeeperGenericTableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	namespace, err := splitNamespacePath(state.Namespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid namespace", err.Error())
		return
	}

	warehouseID := state.WarehouseID.ValueString()
	if warehouseID == "" {
		warehouseID, err = r.client.LookupWarehouseID(ctx, state.ProjectID.ValueString(), state.WarehouseName.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Cannot resolve warehouse", err.Error())
			return
		}
	}

	purge := false
	if !state.PurgeOnDelete.IsNull() && !state.PurgeOnDelete.IsUnknown() {
		purge = state.PurgeOnDelete.ValueBool()
	}

	err = r.client.DeleteGenericTable(ctx, state.ProjectID.ValueString(), warehouseID, namespace, state.Name.ValueString(), purge, false)
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Cannot drop generic table",
			fmt.Sprintf("Lakekeeper refused to drop %q. The table may be protected by a Lakekeeper protection rule; remove the protection and apply again. Server response: %v", state.Name.ValueString(), err),
		)
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *lakekeeperGenericTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitGenericTableImportID(req.ID)
	if len(parts) != 4 {
		resp.Diagnostics.AddError(
			"Invalid import ID format",
			"Expected format: project_id/warehouse_name/namespace/name",
		)
		return
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		resp.Diagnostics.AddError("Invalid import ID format", "project_id must be a UUID")
		return
	}
	if _, err := splitNamespacePath(parts[2]); err != nil {
		resp.Diagnostics.AddError("Invalid import ID format", err.Error())
		return
	}
	if parts[3] == "" || parts[3] == "." || parts[3] == ".." || strings.ContainsAny(parts[3], "\x1f") {
		resp.Diagnostics.AddError("Invalid import ID format", fmt.Sprintf("invalid table name %q", parts[3]))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("warehouse_name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[3])...)
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func splitGenericTableImportID(id string) []string {
	return strings.Split(id, "/")
}

// keepUnlessChanged keeps a computed value across plans while every attribute
// it depends on stays as configured. When one of them changes, the value is
// left unknown so the plan shows that a replacement will produce a new one.
type keepUnlessChanged struct {
	attributes []string
}

func (keepUnlessChanged) Description(_ context.Context) string {
	return "Keeps the prior value unless a replacing attribute changes."
}

func (keepUnlessChanged) MarkdownDescription(_ context.Context) string {
	return "Keeps the prior value unless a replacing attribute changes."
}

func (m keepUnlessChanged) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() {
		return
	}
	for _, attribute := range m.attributes {
		if configChangesAttribute(ctx, &req.State, &req.Config, attribute) {
			return
		}
	}
	resp.PlanValue = req.StateValue
}

// configChangesAttribute reports whether the configuration explicitly sets
// attribute to a value that differs from state. An omitted optional attribute
// counts as unchanged, so import and Optional+Computed omission keep the
// server value.
func configChangesAttribute(ctx context.Context, state *tfsdk.State, config *tfsdk.Config, attribute string) bool {
	p := path.Root(attribute)

	switch attribute {
	case "base_location":
		var stateValue, configValue types.String
		if state.GetAttribute(ctx, p, &stateValue).HasError() || config.GetAttribute(ctx, p, &configValue).HasError() {
			return true
		}
		if configValue.IsNull() || configValue.IsUnknown() {
			return false
		}
		return strings.TrimRight(configValue.ValueString(), "/") != strings.TrimRight(stateValue.ValueString(), "/")
	case "properties":
		var stateValue, configValue types.Map
		if state.GetAttribute(ctx, p, &stateValue).HasError() || config.GetAttribute(ctx, p, &configValue).HasError() {
			return true
		}
		if configValue.IsNull() || configValue.IsUnknown() {
			return false
		}
		return !configValue.Equal(stateValue)
	case "schema":
		var stateValue, configValue jsontypes.Normalized
		if state.GetAttribute(ctx, p, &stateValue).HasError() || config.GetAttribute(ctx, p, &configValue).HasError() {
			return true
		}
		if configValue.IsNull() || configValue.IsUnknown() {
			return false
		}
		equal, diags := configValue.StringSemanticEquals(ctx, stateValue)
		if diags.HasError() {
			return true
		}
		return !equal
	default:
		var stateValue, configValue types.String
		if state.GetAttribute(ctx, p, &stateValue).HasError() || config.GetAttribute(ctx, p, &configValue).HasError() {
			return true
		}
		if configValue.IsNull() || configValue.IsUnknown() {
			return false
		}
		return !configValue.Equal(stateValue)
	}
}

// setGenericTableMetadata copies the table metadata the API returns into model.
// It also normalizes doc, properties and schema, because the server omits them
// when they are empty and returns an empty properties map as no map at all.
func setGenericTableMetadata(ctx context.Context, model *lakekeeperGenericTableResourceModel, table *api.GenericTable) error {
	model.Name = types.StringValue(table.Name)
	model.Format = types.StringValue(table.Format)
	// Lakekeeper stores locations without trailing slashes. Keep the prior
	// value when it differs only in trailing slashes, so a configured location
	// does not change on every refresh and a create keeps the planned value.
	if model.BaseLocation.IsNull() || model.BaseLocation.IsUnknown() ||
		strings.TrimRight(model.BaseLocation.ValueString(), "/") != strings.TrimRight(table.BaseLocation, "/") {
		model.BaseLocation = types.StringValue(table.BaseLocation)
	}
	model.Protected = types.BoolValue(table.Protected)

	if table.Doc == nil {
		model.Doc = types.StringNull()
	} else {
		model.Doc = types.StringValue(*table.Doc)
	}
	if table.Properties == nil {
		// Lakekeeper returns no properties for an empty map. Report an empty
		// map so a configured `properties = {}` does not plan a replacement.
		model.Properties = types.MapValueMust(types.StringType, map[string]attr.Value{})
	} else {
		properties, diags := types.MapValueFrom(ctx, types.StringType, table.Properties)
		if diags.HasError() {
			return fmt.Errorf("could not convert table properties: %v", diags)
		}
		model.Properties = properties
	}
	if table.Schema == nil {
		model.Schema = jsontypes.NewNormalizedNull()
	} else {
		model.Schema = jsontypes.NewNormalizedValue(canonicalJSON(table.Schema))
	}
	return nil
}

// refreshGenericTable loads the table and its UUID and writes both into state.
func (r *lakekeeperGenericTableResource) refreshGenericTable(ctx context.Context, state *lakekeeperGenericTableResourceModel) error {
	namespace, err := splitNamespacePath(state.Namespace.ValueString())
	if err != nil {
		return err
	}

	warehouseID := state.WarehouseID.ValueString()
	if warehouseID == "" {
		warehouseID, err = r.client.LookupWarehouseID(ctx, state.ProjectID.ValueString(), state.WarehouseName.ValueString())
		if err != nil {
			return err
		}
		state.WarehouseID = types.StringValue(warehouseID)
	}

	table, err := r.client.LoadGenericTable(ctx, state.ProjectID.ValueString(), warehouseID, namespace, state.Name.ValueString())
	if err != nil {
		return err
	}
	if table.Name != state.Name.ValueString() {
		return fmt.Errorf("catalog returned table %q instead of %q", table.Name, state.Name.ValueString())
	}

	// Load responses carry no UUID, so resolve it from the listing. Resolving
	// on every refresh notices a table dropped and recreated outside Terraform,
	// which keeps the old UUID otherwise. If the listing is temporarily
	// unavailable, a UUID already in state is better than failing the read.
	genericTableID, err := r.client.ResolveGenericTableID(ctx, state.ProjectID.ValueString(), warehouseID, namespace, table.Name)
	if err != nil {
		if state.GenericTableID.ValueString() == "" {
			return err
		}
	} else {
		state.GenericTableID = types.StringValue(genericTableID)
	}

	if err := setGenericTableMetadata(ctx, state, table); err != nil {
		return err
	}
	state.ID = types.StringValue(genericTableInternalID(state.ProjectID.ValueString(), state.WarehouseName.ValueString(), namespace, table.Name))

	if state.PurgeOnDelete.IsNull() || state.PurgeOnDelete.IsUnknown() {
		state.PurgeOnDelete = types.BoolValue(false)
	}

	return nil
}

// canonicalJSON re-encodes a JSON document with sorted keys, so the state
// stays stable regardless of the server's key order.
func canonicalJSON(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		return string(raw)
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	// Keep <, > and & as written: the default HTML escaping would turn a schema
	// such as `list<float>` into `list\u003cfloat\u003e`.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return string(raw)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
