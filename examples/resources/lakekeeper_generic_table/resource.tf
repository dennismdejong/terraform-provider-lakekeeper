resource "lakekeeper_generic_table" "embeddings" {
  project_id     = "891d18c8-1da4-471e-89f1-6e43eb4dcb38"
  warehouse_name = "warehouse_example"
  namespace      = "ml.images"
  name           = "embeddings"
  format         = "lance"
  doc            = "Image embeddings written by the ML pipeline"

  properties = {
    owner = "ml-platform"
  }

  schema = jsonencode({
    type = "struct"
    fields = [
      { name = "id", type = "long" },
      { name = "embedding", type = "list<float>" },
    ]
  })
}
