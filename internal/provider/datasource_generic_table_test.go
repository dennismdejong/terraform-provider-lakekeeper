//go:build acceptance

package provider

import (
	"fmt"
	"math/rand"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/testutil"
)

func TestAccDataLakekeeperGenericTable_basic(t *testing.T) {
	project := testutil.CreateProject(t)

	keyPrefix := fmt.Sprintf("key-prefix-%d", rand.Int())
	warehouse := testutil.CreateWarehouse(t, project.ID, keyPrefix)
	testutil.CreateNamespace(t, project.ID, warehouse, "dsacc")

	name := acctest.RandString(8)
	tableID := testutil.CreateGenericTable(t, project.ID, warehouse, []string{"dsacc"}, name, "dataset")
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "lakekeeper_generic_table" "this" {
  project_id   = %q
  warehouse_id = %q
  namespace    = "dsacc"
  name         = %q
}
`, project.ID, warehouse.ID, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "id", fmt.Sprintf("%s/%s/dsacc/%s", project.ID, warehouse.ID, name)),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "generic_table_id", tableID),
					resource.TestMatchResourceAttr("data.lakekeeper_generic_table.this", "generic_table_id", uuidPattern),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "format", "dataset"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "namespace", "dsacc"),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "name", name),
					resource.TestCheckResourceAttr("data.lakekeeper_generic_table.this", "protected", "false"),
				),
			},
		},
	})
}
