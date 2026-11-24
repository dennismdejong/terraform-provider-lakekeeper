//go:build acceptance

package provider

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/testutil"
)

func TestAccLakekeeperGenericTable_basic(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "gtacc")
	testutil.CreateNamespace(t, project.ID, warehouse, "gtacc", "nested")
	testutil.CreateNamespace(t, project.ID, warehouse, "gtacc", "other")

	rName := acctest.RandString(8)
	renamed := acctest.RandString(8)
	baseLocation := fmt.Sprintf("s3://testacc/%s/generic-tables/%s", keyPrefix, rName)
	changedBaseLocation := baseLocation + "-changed"
	replacementBaseLocation := baseLocation + "-delta"
	movedBaseLocation := baseLocation + "-moved"
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	internalID := func(name string) string {
		return fmt.Sprintf("%s/%s/gtacc.nested/%s", project.ID, warehouse.Name, name)
	}

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLakekeeperGenericTableDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtacc.nested", rName, "lance", baseLocation, `
				doc        = "first"
				properties = { owner = "data" }
				schema     = jsonencode({ type = "struct", fields = [] })`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", internalID(rName)),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "name", rName),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "namespace", "gtacc.nested"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "format", "lance"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "warehouse_id", warehouse.ID),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "base_location", baseLocation),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "doc", "first"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "properties.owner", "data"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "schema", `{"fields":[],"type":"struct"}`),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "protected", "false"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "purge_on_delete", "false"),
					resource.TestMatchResourceAttr("lakekeeper_generic_table.this", "generic_table_id", uuidPattern),
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Renaming with base_location omitted must not replace the table.
				Config: testAccGenericTableConfigWithoutBaseLocation(project.ID, warehouse.Name, "gtacc.nested", renamed, "lance"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", internalID(renamed)),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "name", renamed),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "base_location", baseLocation),
					resource.TestMatchResourceAttr("lakekeeper_generic_table.this", "generic_table_id", uuidPattern),
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Changing only base_location must replace the table.
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtacc.nested", renamed, "lance", changedBaseLocation, `
				doc        = "first"
				properties = { owner = "data" }
				schema     = jsonencode({ type = "struct", fields = [] })`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "base_location", changedBaseLocation),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", internalID(renamed)),
					resource.TestMatchResourceAttr("lakekeeper_generic_table.this", "generic_table_id", uuidPattern),
				),
			},
			{
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtacc.nested", renamed, "delta", replacementBaseLocation, `
				doc        = ""
				properties = {}
				schema     = jsonencode({ type = "struct", fields = [] })`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "format", "delta"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "doc", ""),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "properties.#", "0"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", internalID(renamed)),
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtacc.other", renamed, "delta", movedBaseLocation, `
				doc        = ""
				properties = {}
				schema     = jsonencode({ type = "struct", fields = [] })`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "namespace", "gtacc.other"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", fmt.Sprintf("%s/%s/gtacc.other/%s", project.ID, warehouse.Name, renamed)),
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccGenericTableConfig(projectID, warehouseName, namespace, name, format, baseLocation, extra string) string {
	return fmt.Sprintf(`
resource "lakekeeper_generic_table" "this" {
  project_id     = %q
  warehouse_name = %q
  namespace      = %q
  name           = %q
  format         = %q
  base_location  = %q
%s
}
`, projectID, warehouseName, namespace, name, format, baseLocation, extra)
}

func testAccGenericTableConfigWithoutBaseLocation(projectID, warehouseName, namespace, name, format string) string {
	return fmt.Sprintf(`
resource "lakekeeper_generic_table" "this" {
  project_id     = %q
  warehouse_name = %q
  namespace      = %q
  name           = %q
  format         = %q
  doc            = "first"
  properties     = { owner = "data" }
  schema         = jsonencode({ type = "struct", fields = [] })
}
`, projectID, warehouseName, namespace, name, format)
}

func testAccGenericTableMinimalConfig(projectID, warehouseName, namespace, name, format string) string {
	return fmt.Sprintf(`
resource "lakekeeper_generic_table" "this" {
  project_id     = %q
  warehouse_name = %q
  namespace      = %q
  name           = %q
  format         = %q
}
`, projectID, warehouseName, namespace, name, format)
}

// TestAccLakekeeperGenericTable_omittedOptionalAttributes creates a table with
// doc, properties, schema and base_location omitted. The create response must
// fill them in, or the apply fails with unknown values in state.
func TestAccLakekeeperGenericTable_omittedOptionalAttributes(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "gtmin")
	testutil.CreateNamespace(t, project.ID, warehouse, "gtmin", "other")

	name := acctest.RandString(8)
	var firstLocation string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLakekeeperGenericTableDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccGenericTableMinimalConfig(project.ID, warehouse.Name, "gtmin", name, "lance"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("lakekeeper_generic_table.this", "base_location", regexp.MustCompile(`^s3://`)),
					resource.TestCheckNoResourceAttr("lakekeeper_generic_table.this", "doc"),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "properties.#", "0"),
					resource.TestCheckNoResourceAttr("lakekeeper_generic_table.this", "schema"),
					func(state *terraform.State) error {
						firstLocation = state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes["base_location"]
						return nil
					},
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// The omitted base_location must be derived again for the new namespace.
				Config: testAccGenericTableMinimalConfig(project.ID, warehouse.Name, "gtmin.other", name, "lance"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "namespace", "gtmin.other"),
					func(state *terraform.State) error {
						got := state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes["base_location"]
						if got == firstLocation {
							return fmt.Errorf("base_location stayed %q after the namespace changed", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccLakekeeperGenericTable_refreshesIDAfterExternalRecreate drops and
// recreates the table outside Terraform under the same name. The refresh must
// pick up the new UUID instead of keeping the old one.
func TestAccLakekeeperGenericTable_refreshesIDAfterExternalRecreate(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "gtrecreate")

	name := acctest.RandString(8)
	namespace := []string{"gtrecreate"}
	baseLocation := fmt.Sprintf("s3://testacc/%s/generic-tables/%s", keyPrefix, name)
	config := testAccGenericTableConfig(project.ID, warehouse.Name, "gtrecreate", name, "lance", baseLocation, "")

	var firstID string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLakekeeperGenericTableDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(state *terraform.State) error {
					firstID = state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes["generic_table_id"]
					ctx := t.Context()
					if err := testutil.TestLakekeeperClient.DeleteGenericTable(ctx, project.ID, warehouse.ID, namespace, name, false, true); err != nil {
						return fmt.Errorf("could not drop the table outside Terraform: %w", err)
					}
					if _, err := testutil.TestLakekeeperClient.CreateGenericTable(ctx, project.ID, warehouse.ID, namespace, api.GenericTableRequest{
						Name:         name,
						Format:       "lance",
						BaseLocation: &baseLocation,
					}); err != nil {
						return fmt.Errorf("could not recreate the table outside Terraform: %w", err)
					}
					return nil
				},
			},
			{
				RefreshState: true,
				Check: func(state *terraform.State) error {
					got := state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes["generic_table_id"]
					if got == firstID {
						return fmt.Errorf("generic_table_id %q was not refreshed after the table was recreated", got)
					}
					want, err := testutil.TestLakekeeperClient.ResolveGenericTableID(t.Context(), project.ID, warehouse.ID, namespace, name)
					if err != nil {
						return err
					}
					if got != want {
						return fmt.Errorf("generic_table_id = %q, want %q", got, want)
					}
					return nil
				},
			},
		},
	})
}

// TestAccLakekeeperGenericTable_baseLocationTrailingSlash ensures a location
// written with a trailing slash applies and stays equivalent when the slash is
// dropped, even though Lakekeeper stores locations without one.
func TestAccLakekeeperGenericTable_baseLocationTrailingSlash(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "gtslash")

	name := acctest.RandString(8)
	baseLocation := fmt.Sprintf("s3://testacc/%s/generic-tables/%s", keyPrefix, name)

	var genericTableID string
	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckLakekeeperGenericTableDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtslash", name, "lance", baseLocation+"/", ""),
				Check: func(state *terraform.State) error {
					attributes := state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes
					genericTableID = attributes["generic_table_id"]
					if got := attributes["base_location"]; got != baseLocation+"/" {
						return fmt.Errorf("base_location = %q, want %q", got, baseLocation+"/")
					}
					return nil
				},
			},
			{
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtslash", name, "lance", baseLocation, ""),
				Check: func(state *terraform.State) error {
					attributes := state.RootModule().Resources["lakekeeper_generic_table.this"].Primary.Attributes
					if got := attributes["generic_table_id"]; got != genericTableID {
						return fmt.Errorf("generic_table_id changed from %q to %q, the table was replaced", genericTableID, got)
					}
					return nil
				},
			},
		},
	})
}

func testAccCheckLakekeeperGenericTableDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "lakekeeper_generic_table" {
			continue
		}

		parts := strings.Split(rs.Primary.ID, "/")
		if len(parts) != 4 {
			return fmt.Errorf("unexpected generic table ID %q", rs.Primary.ID)
		}
		projectID, warehouseName := parts[0], parts[1]
		namespace := strings.Split(parts[2], ".")
		name := parts[3]

		ctx := context.Background()
		warehouseID, err := testutil.TestLakekeeperClient.LookupWarehouseID(ctx, projectID, warehouseName)
		if err != nil {
			return fmt.Errorf("could not resolve warehouse: %w", err)
		}

		tables, err := testutil.TestLakekeeperClient.ListGenericTables(ctx, projectID, warehouseID, namespace)
		if err != nil {
			return fmt.Errorf("could not list generic tables: %w", err)
		}
		for _, table := range tables {
			if table.Name == name && slices.Equal(table.Namespace, namespace) {
				return fmt.Errorf("generic table still exists")
			}
		}
		return nil
	}
	return nil
}
