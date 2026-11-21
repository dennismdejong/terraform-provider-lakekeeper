//go:build acceptance

package provider

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/testutil"
)

func TestAccDataLakekeeperGenericTables_basic(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "listacc")

	testutil.CreateGenericTable(t, project.ID, warehouse, []string{"listacc"}, "alpha", "dataset")
	testutil.CreateGenericTable(t, project.ID, warehouse, []string{"listacc"}, "beta", "lance")

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "lakekeeper_generic_tables" "this" {
  project_id   = %q
  warehouse_id = %q
  namespace    = "listacc"
}
`, project.ID, warehouse.ID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.#", "2"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.0.name", "alpha"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.0.format", "dataset"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.0.namespace.#", "1"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.0.namespace.0", "listacc"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.1.name", "beta"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_tables.this", "tables.1.format", "lance"),
				),
			},
		},
	})
}
