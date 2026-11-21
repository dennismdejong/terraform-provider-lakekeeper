# id is "{{project_id}}/{{warehouse_name}}/{{namespace}}/{{name}}"
# Omit doc, properties and schema (or match the stored values) after import:
# setting different values replaces the table. purge_on_delete defaults to
# false, so a replacement keeps the data files.
terraform import lakekeeper_generic_table.embeddings "891d18c8-1da4-471e-89f1-6e43eb4dcb38/warehouse_example/ml.images/embeddings"
