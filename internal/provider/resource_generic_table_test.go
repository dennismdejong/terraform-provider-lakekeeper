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
				Config: testAccGenericTableConfig(project.ID, warehouse.Name, "gtacc.nested", renamed, "lance", baseLocation, `
				doc        = "first"
				properties = { owner = "data" }
				schema     = jsonencode({ type = "struct", fields = [] })`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "id", internalID(renamed)),
					resource.TestCheckResourceAttr("lakekeeper_generic_table.this", "name", renamed),
					resource.TestMatchResourceAttr("lakekeeper_generic_table.this", "generic_table_id", uuidPattern),
				),
			},
			{
				ResourceName:      "lakekeeper_generic_table.this",
				ImportState:       true,
				ImportStateVerify: true,
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
