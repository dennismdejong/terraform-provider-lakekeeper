package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lakekeeper/terraform-provider-lakekeeper/internal/provider/api"
)

// splitNamespacePath turns a dotted Terraform namespace into the levels the
// generic table API expects. A level cannot contain a dot, slash or the
// multi-level wire separator, because those would make the path ambiguous.
func splitNamespacePath(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("namespace must not be empty")
	}
	parts := strings.Split(path, ".")
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("namespace %q has an empty level", path)
		}
		if strings.ContainsAny(part, "/\x1f") {
			return nil, fmt.Errorf("namespace level %q must not contain '/' or the multi-level separator", part)
		}
	}
	return parts, nil
}

func joinNamespacePath(parts []string) string {
	return strings.Join(parts, ".")
}

func genericTableInternalID(projectID, warehouseName string, namespace []string, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s", projectID, warehouseName, joinNamespacePath(namespace), name)
}

// verifyWarehouse checks that warehouseID exists, belongs to projectID and is
// active, so a data source cannot read another project's warehouse.
func verifyWarehouse(ctx context.Context, client *api.Client, projectID, warehouseID string) error {
	for _, id := range []string{projectID, warehouseID} {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("project and warehouse must be UUIDs")
		}
	}
	w, err := client.GetWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return err
	}
	if w.ID != warehouseID || w.ProjectID != projectID || !w.IsActive() {
		return fmt.Errorf("warehouse is inactive or does not belong to the expected project")
	}
	return nil
}
